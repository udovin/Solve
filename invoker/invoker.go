// Package invoker represents solve implementation for running asynchronious
// tasks like compiling and judging solutions.
package invoker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/opencontainers/runc/libcontainer"

	"github.com/udovin/solve/core"
	"github.com/udovin/solve/managers"
	"github.com/udovin/solve/models"
	"github.com/udovin/solve/pkg"
	"github.com/udovin/solve/pkg/polygon"
)

// Invoker represents manager for asynchronous actions (invocations).
type Invoker struct {
	core    *core.Core
	files   *managers.FileManager
	factory libcontainer.Factory
}

// New creates a new instance of Invoker.
func New(c *core.Core) *Invoker {
	return &Invoker{
		core:  c,
		files: managers.NewFileManager(c),
	}
}

// Start starts invoker daemons.
//
// This function will spawn config.Invoker.Threads amount of goroutines.
func (s *Invoker) Start() error {
	if s.factory != nil {
		return fmt.Errorf("factory already created")
	}
	factory, err := libcontainer.New(
		"/tmp/libcontainer",
		libcontainer.InitArgs(os.Args[0], "init"),
	)
	if err != nil {
		return err
	}
	s.factory = factory
	threads := s.core.Config.Invoker.Threads
	if threads <= 0 {
		threads = 1
	}
	for i := 0; i < threads; i++ {
		name := fmt.Sprintf("invoker-%d", i+1)
		s.core.StartTask(name, s.runDaemon)
	}
	return nil
}

func (s *Invoker) runDaemon(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if ok := s.runDaemonTick(ctx); !ok {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}
	}
}

func (s *Invoker) runDaemonTick(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
	}
	task, err := s.core.Tasks.PopQueued(ctx, isSupportedTask)
	if err != nil {
		if err != sql.ErrNoRows {
			s.core.Logger().Error("Error", err)
		}
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			task.Status = models.FailedTask
			s.core.Logger().Error("Task panic", r)
			panic(r)
		}
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(task.ExpireTime, 0))
		defer cancel()
		if err := s.core.Tasks.Update(ctx, task); err != nil {
			s.core.Logger().Error("Error", err)
		}
	}()
	var waiter sync.WaitGroup
	defer waiter.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	waiter.Add(1)
	go func() {
		defer waiter.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case <-ctx.Done():
					return
				default:
				}
				if time.Now().After(time.Unix(task.ExpireTime, 0)) {
					s.core.Logger().Error("Task expired", core.Any("task", task.ID))
					return
				}
				clone := task
				clone.ExpireTime = time.Now().Add(5 * time.Second).Unix()
				if err := s.core.Tasks.Update(ctx, clone); err != nil {
					s.core.Logger().Warn(
						"Unable to ping task",
						core.Any("task", task.ID),
						err,
					)
				} else {
					task.ExpireTime = clone.ExpireTime
				}
			}
		}
	}()
	err = s.onTask(ctx, task)
	cancel()
	waiter.Wait()
	if err != nil {
		s.core.Logger().Error("Task failed", err)
		task.Status = models.FailedTask
	} else {
		task.Status = models.SucceededTask
	}
	return true
}

func (s *Invoker) onTask(ctx context.Context, task models.Task) error {
	s.core.Logger().Debugf("Received new task %d", task.ID)
	switch task.Kind {
	case models.JudgeSolutionTask:
		return s.onJudgeSolution(ctx, task)
	default:
		s.core.Logger().Errorf("Unknown task kind %d", task.Kind)
		return fmt.Errorf("unknown task")
	}
}

func (s *Invoker) getSolution(id int64) (models.Solution, error) {
	solution, err := s.core.Solutions.Get(id)
	if err == sql.ErrNoRows {
		if err := s.core.Solutions.Sync(context.Background()); err != nil {
			return models.Solution{}, fmt.Errorf(
				"unable to sync solutions: %w", err,
			)
		}
		solution, err = s.core.Solutions.Get(id)
	}
	return solution, err
}

func (s *Invoker) onJudgeSolution(ctx context.Context, task models.Task) error {
	var taskConfig models.JudgeSolutionTaskConfig
	if err := task.ScanConfig(&taskConfig); err != nil {
		return fmt.Errorf("unable to scan task config: %w", err)
	}
	solution, err := s.getSolution(taskConfig.SolutionID)
	if err != nil {
		return fmt.Errorf("unable to fetch task solution: %w", err)
	}
	problem, err := s.core.Problems.Get(solution.ProblemID)
	if err != nil {
		return fmt.Errorf("unable to fetch task problem: %w", err)
	}
	report := models.SolutionReport{
		Verdict: models.Rejected,
	}
	defer func() {
		if err := solution.SetReport(&report); err != nil {
			s.core.Logger().Error(err)
			return
		}
		s.core.Logger().Info("Report", core.Any("report", report))
		if err := s.core.Solutions.Update(ctx, solution); err != nil {
			s.core.Logger().Error(err)
			return
		}
	}()
	tempDir, err := makeTempDir()
	if err != nil {
		return err
	}
	s.core.Logger().Debugf("Temp dir: %s", tempDir)
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()
	problemFile, err := s.files.DownloadFile(ctx, problem.PackageID)
	if err != nil {
		return err
	}
	defer problemFile.Close()
	tempProblemPath := filepath.Join(tempDir, "problem")
	if err := pkg.ExtractZip(problemFile.Name(), tempProblemPath); err != nil {
		return err
	}
	compierPath := filepath.Join(
		s.core.Config.Storage.FilesDir,
		"dosbox-tasm.tar.gz",
	)
	solutionFile, err := s.files.DownloadFile(ctx, int64(solution.ContentID))
	if err != nil {
		return err
	}
	defer solutionFile.Close()
	tempSolutionPath := filepath.Join(tempDir, "solution.txt")
	tempCompileLogPath := filepath.Join(tempDir, "compile_log.txt")
	tempImagePath := filepath.Join(tempDir, "rootfs")
	if err := pkg.ExtractTarGz(compierPath, tempImagePath); err != nil {
		return err
	}
	compier := compiler{
		Logger:            s.core.Logger(),
		Factory:           s.factory,
		ImagePath:         tempImagePath,
		CompileArgs:       []string{"dosbox", "-conf", "/dosbox_compile.conf"},
		CompileCwd:        "/home/solution",
		CompileEnv:        defaultEnv,
		CompileSourcePath: "/home/solution/solution.asm",
		CompileTargetPath: "/home/solution/SOLUTION.EXE",
		CompileLogPath:    "/home/solution/COMPLIE.LOG",
		ExecuteArgs:       []string{"dosbox", "-conf", "/dosbox_execute.conf"},
		ExecuteCwd:        "/home/solution",
		ExecuteEnv:        defaultEnv,
		ExecuteBinaryPath: "/home/solution/SOLUTION.EXE",
		ExecuteInputPath:  "/home/solution/input.txt",
		ExecuteOutputPath: "/home/solution/OUTPUT.TXT",
	}
	if err := compier.Compile(
		ctx, solutionFile.Name(), tempSolutionPath, tempCompileLogPath,
	); err != nil {
		s.core.Logger().Warn(
			"Unable to compile",
			err,
		)
		compileLog, err := readFile(tempCompileLogPath, 1024)
		if err != nil {
			s.core.Logger().Warn(
				"Unable to read compile logs",
				err,
			)
		}
		report.CompileLog = compileLog
		report.Verdict = models.CompilationError
		return err
	} else {
		compileLog, err := readFile(tempCompileLogPath, 1024)
		if err != nil {
			s.core.Logger().Warn(
				"Unable to read compile logs",
				err,
			)
		}
		report.CompileLog = compileLog
	}
	pkg, err := polygon.ReadProblem(tempProblemPath)
	if err != nil {
		return fmt.Errorf("unable to parse package: %w", err)
	}
	for _, testSet := range pkg.TestSets {
		for i := range testSet.Tests {
			testInput := fmt.Sprintf(testSet.InputPathPattern, i+1)
			testAnswer := fmt.Sprintf(testSet.AnswerPathPattern, i+1)
			inputPath := filepath.Join(tempProblemPath, testInput)
			answerPath := filepath.Join(tempProblemPath, testAnswer)
			tempOutputPath := filepath.Join(tempDir, fmt.Sprintf("output-%d.txt", len(report.Tests)))
			inputText, err := readFile(inputPath, 1024)
			if err != nil {
				s.core.Logger().Error("Error", err)
				inputText = ""
			}
			if err := compier.Execute(
				ctx, tempSolutionPath, inputPath, tempOutputPath,
				5*time.Second, 128*1024*1024,
			); err == nil {
				outputText, err := readFile(tempOutputPath, 1024)
				if err != nil {
					s.core.Logger().Error("Error", err)
					outputText = ""
				}
				message, ok, err := compareFiles(tempOutputPath, answerPath)
				if err != nil {
					report.Tests = append(report.Tests, models.TestReport{
						Verdict:  models.Rejected,
						CheckLog: fmt.Sprintf("unable to compare files: %s", err.Error()),
						Input:    inputText,
						Output:   outputText,
					})
				} else if ok {
					report.Tests = append(report.Tests, models.TestReport{
						Verdict:  models.Accepted,
						CheckLog: message,
						Input:    inputText,
						Output:   outputText,
					})
				} else {
					report.Tests = append(report.Tests, models.TestReport{
						Verdict:  models.WrongAnswer,
						CheckLog: message,
						Input:    inputText,
						Output:   outputText,
					})
				}
			} else if errors.Is(err, context.DeadlineExceeded) {
				report.Tests = append(report.Tests, models.TestReport{
					Verdict: models.TimeLimitExceeded,
					Input:   inputText,
				})
			} else if state, ok := err.(exitCodeError); ok {
				report.Tests = append(report.Tests, models.TestReport{
					Verdict:  models.RuntimeError,
					CheckLog: fmt.Sprintf("Exit code: %d", state.ExitCode()),
					Input:    inputText,
				})
			} else {
				report.Tests = append(report.Tests, models.TestReport{
					Verdict:  models.Rejected,
					CheckLog: fmt.Sprint("Unknown error: %w", err),
					Input:    inputText,
				})
			}
		}
	}
	report.Verdict = models.Accepted
	for _, test := range report.Tests {
		if test.Verdict != models.Accepted {
			report.Verdict = models.PartiallyAccepted
			break
		}
	}
	return nil
}

func readFile(name string, limit int) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	bytes := make([]byte, limit+1)
	read, err := file.Read(bytes)
	if err != nil {
		return "", err
	}
	if read > limit {
		return strings.ToValidUTF8(string(bytes[:limit]), "") + "...", nil
	}
	return strings.ToValidUTF8(string(bytes[:read]), ""), nil
}

func compareFiles(outputPath, answerPath string) (string, bool, error) {
	output, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return "", false, err
	}
	answer, err := ioutil.ReadFile(answerPath)
	if err != nil {
		return "", false, err
	}
	outputStr := string(output)
	outputStr = strings.ReplaceAll(outputStr, "\n", "")
	outputStr = strings.ReplaceAll(outputStr, "\r", "")
	outputStr = strings.ReplaceAll(outputStr, "\t", "")
	outputStr = strings.ReplaceAll(outputStr, " ", "")
	answerStr := string(answer)
	answerStr = strings.ReplaceAll(answerStr, "\n", "")
	answerStr = strings.ReplaceAll(answerStr, "\r", "")
	answerStr = strings.ReplaceAll(answerStr, "\t", "")
	answerStr = strings.ReplaceAll(answerStr, " ", "")
	if outputStr == answerStr {
		return "ok", true, nil
	} else {
		if len(output) > 100 {
			output = output[:100]
		}
		if len(answer) > 100 {
			answer = answer[:100]
		}
		return fmt.Sprintf("expected %q, got %q", string(answer), string(output)), false, nil
	}
}
