package invoker

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/udovin/solve/models"
	"github.com/udovin/solve/pkg"
	"github.com/udovin/solve/pkg/logs"
	"github.com/udovin/solve/pkg/polygon"
)

func extractPolygonProblem(
	source, target string,
	compilers *compilerManager,
) (Problem, error) {
	if err := pkg.ExtractZip(source, target); err != nil {
		return nil, fmt.Errorf("cannot extract problem: %w", err)
	}
	config, err := polygon.ReadProblemConfig(
		filepath.Join(target, "problem.xml"),
	)
	if err != nil {
		_ = os.RemoveAll(target)
		return nil, fmt.Errorf("cannot read problem config: %w", err)
	}
	return &polygonProblem{
		path:      target,
		config:    config,
		compilers: compilers,
	}, nil
}

type compiled struct {
	path     string
	compiler Compiler
}

type polygonProblem struct {
	path        string
	config      polygon.Problem
	compilers   *compilerManager
	executables map[string]compiled
}

func (p *polygonProblem) Compile(ctx context.Context) error {
	p.executables = map[string]compiled{}
	for _, executable := range p.config.Files.Executables {
		if executable.Source == nil {
			continue
		}
		name, ok := polygonCompilers[executable.Source.Type]
		if !ok {
			name = "polygon." + executable.Source.Type
		}
		compiler, err := p.compilers.GetCompiler(ctx, name)
		if err != nil {
			return err
		}
		source := executable.Source.Path
		target := strings.TrimSuffix(source, filepath.Ext(source))
		sourcePath := filepath.Join(p.path, source)
		targetPath := filepath.Join(p.path, target)
		testlibPath := filepath.Join(p.path, "files/testlib.h")
		report, err := compiler.Compile(ctx, CompileOptions{
			Source: sourcePath,
			Target: targetPath,
			InputFiles: []MountFile{
				{Source: testlibPath, Target: "testlib.h"},
			},
		})
		if err != nil {
			return err
		}
		if !report.Success() {
			return fmt.Errorf(
				"cannot compile %q with compiler %q",
				executable.Source.Path, name,
			)
		}
		p.compilers.logger.Debug(
			"Compiled executable",
			logs.Any("path", source),
		)
		p.executables[target] = compiled{
			path:     targetPath,
			compiler: compiler,
		}
	}
	var mainSolution polygon.Solution
	for _, solution := range p.config.Assets.Solutions {
		if solution.Tag == "main" {
			mainSolution = solution
		}
	}
	if mainSolution.Source == nil {
		return fmt.Errorf("cannot find main solution")
	}
	var solution compiled
	{
		name, ok := polygonCompilers[mainSolution.Source.Type]
		if !ok {
			name = "polygon." + mainSolution.Source.Type
		}
		compiler, err := p.compilers.GetCompiler(ctx, name)
		if err != nil {
			return err
		}
		sourcePath := filepath.Join(p.path, mainSolution.Source.Path)
		targetPath := strings.TrimSuffix(sourcePath, filepath.Ext(sourcePath))
		report, err := compiler.Compile(
			ctx, CompileOptions{
				Source: sourcePath,
				Target: targetPath,
			})
		if err != nil {
			return err
		}
		if !report.Success() {
			return fmt.Errorf(
				"cannot compile %q with compiler %q",
				mainSolution.Source.Path, name,
			)
		}
		p.compilers.logger.Debug(
			"Compiled solution",
			logs.Any("path", mainSolution.Source.Path),
		)
		solution = compiled{
			path:     targetPath,
			compiler: compiler,
		}
	}
	for _, testSet := range p.config.TestSets {
		for i, test := range testSet.Tests {
			input := fmt.Sprintf(testSet.InputPathPattern, i+1)
			answer := fmt.Sprintf(testSet.AnswerPathPattern, i+1)
			if test.Cmd != "" {
				args := strings.Fields(test.Cmd)
				if len(args) == 0 {
					return fmt.Errorf("cannot find executable")
				}
				executable, ok := p.executables[fmt.Sprintf("files/%s", args[0])]
				if !ok {
					return fmt.Errorf("cannot find executable: %q", args[0])
				}
				report, err := executable.compiler.Execute(ctx, ExecuteOptions{
					Binary: executable.path,
					Args:   args[1:],
					OutputFiles: []MountFile{
						{Source: filepath.Join(p.path, input), Target: "stdout"},
					},
				})
				if err != nil {
					return fmt.Errorf("cannot execute generator %q: %w", args[0], err)
				}
				if !report.Success() {
					return fmt.Errorf("generator exited with code: %v", report.ExitCode)
				}
			}
			{
				report, err := solution.compiler.Execute(ctx, ExecuteOptions{
					Binary: solution.path,
					InputFiles: []MountFile{
						{Source: filepath.Join(p.path, input), Target: "stdin"},
					},
					OutputFiles: []MountFile{
						{Source: filepath.Join(p.path, answer), Target: "stdout"},
					},
				})
				if err != nil {
					return fmt.Errorf("cannot execute solution: %w", err)
				}
				if !report.Success() {
					return fmt.Errorf("solution exited with code: %v", report.ExitCode)
				}
			}
		}
	}
	return nil
}

func (p *polygonProblem) GetTestGroups() ([]ProblemTestGroup, error) {
	var groups []ProblemTestGroup
	for _, testSet := range p.config.TestSets {
		groups = append(groups, &polygonProblemTestGroup{
			problem: p,
			config:  testSet,
		})
	}
	return groups, nil
}

func (p *polygonProblem) GetStatements() ([]ProblemStatement, error) {
	var statements []ProblemStatement
	for _, statement := range p.config.Statements {
		if statement.Type != "application/x-tex" {
			continue
		}
		if _, ok := polygonLocales[statement.Language]; !ok {
			continue
		}
		statements = append(statements, &polygonProblemStatement{
			problem:  p,
			language: statement.Language,
		})
	}
	return statements, nil
}

type polygonProblemTestGroup struct {
	problem *polygonProblem
	config  polygon.TestSet
}

func (g *polygonProblemTestGroup) Name() string {
	return g.config.Name
}

func (g *polygonProblemTestGroup) TimeLimit() int64 {
	return g.config.TimeLimit
}

func (g *polygonProblemTestGroup) MemoryLimit() int64 {
	return g.config.MemoryLimit
}

func (g *polygonProblemTestGroup) GetTests() ([]ProblemTest, error) {
	var tests []ProblemTest
	for i, config := range g.config.Tests {
		input := fmt.Sprintf(g.config.InputPathPattern, i+1)
		answer := fmt.Sprintf(g.config.AnswerPathPattern, i+1)
		tests = append(tests, polygonProblemTest{
			config:     config,
			inputPath:  filepath.Join(g.problem.path, input),
			answerPath: filepath.Join(g.problem.path, answer),
		})
	}
	return tests, nil
}

type polygonProblemTest struct {
	inputPath  string
	answerPath string
	config     polygon.Test
}

func (t polygonProblemTest) OpenInput() (*os.File, error) {
	return os.Open(t.inputPath)
}

func (t polygonProblemTest) OpenAnswer() (*os.File, error) {
	return os.Open(t.answerPath)
}

type polygonProblemStatement struct {
	problem  *polygonProblem
	language string
}

func (s *polygonProblemStatement) Locale() string {
	return polygonLocales[s.language]
}

func (s *polygonProblemStatement) GetConfig() (models.ProblemStatementConfig, error) {
	statement, err := polygon.ReadProblemStatementConfig(filepath.Join(
		s.problem.path, "statements", s.language, "problem-properties.json",
	))
	if err != nil {
		return models.ProblemStatementConfig{}, err
	}
	config := models.ProblemStatementConfig{
		Locale: s.Locale(),
		Title:  statement.Name,
		Legend: statement.Legend,
		Input:  statement.Input,
		Output: statement.Output,
		Notes:  statement.Notes,
	}
	for _, sample := range statement.SampleTests {
		config.Samples = append(
			config.Samples,
			models.ProblemStatementSample{
				Input:  sample.Input,
				Output: sample.Output,
			},
		)
	}
	return config, nil
}

func (s *polygonProblemStatement) GetResources() ([]ProblemResource, error) {
	config, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	resourcesDir := filepath.Join(s.problem.path, "statements", s.language)
	files, err := os.ReadDir(resourcesDir)
	if err != nil {
		return nil, err
	}
	resources := []ProblemResource{}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		inStatements := strings.Contains(config.Title, name) ||
			strings.Contains(config.Legend, name) ||
			strings.Contains(config.Input, name) ||
			strings.Contains(config.Output, name) ||
			strings.Contains(config.Notes, name)
		if !inStatements {
			continue
		}
		resources = append(resources, polygonProblemResource{
			path: filepath.Join(resourcesDir, name),
			name: name,
		})
	}
	return resources, nil
}

type polygonProblemResource struct {
	path string
	name string
}

func (p polygonProblemResource) Name() string {
	return p.name
}

func (p polygonProblemResource) GetMD5() (string, error) {
	file, err := os.Open(p.path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := md5.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (p polygonProblemResource) Open() (*os.File, error) {
	return os.Open(p.path)
}

var polygonCompilers = map[string]string{
	"cpp.g++17": "cpp17-gcc",
}

var polygonLocales = map[string]string{
	"russian": "ru",
	"english": "en",
}
