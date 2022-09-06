package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/udovin/solve/managers"
	"github.com/udovin/solve/models"
)

// registerSolutionHandlers registers handlers for solution management.
func (v *View) registerSolutionHandlers(g *echo.Group) {
	if v.core.Users == nil {
		return
	}
	g.GET(
		"/v0/solutions", v.observeSolutions,
		v.extractAuth(v.sessionAuth, v.guestAuth),
		v.requirePermission(models.ObserveSolutionsRole),
	)
	g.GET(
		"/v0/solutions/:solution", v.observeSolution,
		v.extractAuth(v.sessionAuth, v.guestAuth), v.extractSolution,
		v.requirePermission(models.ObserveSolutionRole),
	)
}

type Solution struct {
	ID         int64           `json:"id"`
	Problem    *Problem        `json:"problem"`
	User       *User           `json:"user"`
	Report     *SolutionReport `json:"report"`
	CreateTime int64           `json:"create_time"`
}

type Solutions struct {
	Solutions []Solution `json:"solutions"`
}

type solutionSorter []Solution

func (v solutionSorter) Len() int {
	return len(v)
}

func (v solutionSorter) Less(i, j int) bool {
	return v[i].ID > v[j].ID
}

func (v solutionSorter) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

func (v *View) tryFindSolutionTask(id int64) (models.Task, error) {
	tasks, err := v.core.Tasks.FindByStatus(models.QueuedTask, models.RunningTask)
	if err != nil {
		return models.Task{}, err
	}
	for _, task := range tasks {
		if task.Kind == models.JudgeSolutionTask {
			var config models.JudgeSolutionTaskConfig
			if err := task.ScanConfig(&config); err != nil {
				continue
			}
			if config.SolutionID == id {
				return task, nil
			}
		}
	}
	return models.Task{}, sql.ErrNoRows
}

func (v *View) findSolutionTask(c echo.Context, id int64) (models.Task, error) {
	tasks, err := v.tryFindSolutionTask(id)
	if err == sql.ErrNoRows {
		if err := v.core.Tasks.Sync(getContext(c)); err != nil {
			return models.Task{}, err
		}
		return v.tryFindSolutionTask(id)
	}
	return tasks, err
}

func (v *View) makeSolutionReport(c echo.Context, solution models.Solution, withLogs bool) *SolutionReport {
	report, err := solution.GetReport()
	if err != nil {
		return &SolutionReport{
			Verdict: models.FailedTask.String(),
		}
	}
	if report == nil {
		task, err := v.findSolutionTask(c, solution.ID)
		if err != nil {
			return &SolutionReport{
				Verdict: models.FailedTask.String(),
			}
		}
		return &SolutionReport{
			Verdict: task.Status.String(),
		}
	}
	resp := SolutionReport{
		Verdict: report.Verdict.String(),
	}
	if withLogs {
		resp.CompileLog = report.CompileLog
		for _, test := range report.Tests {
			resp.Tests = append(resp.Tests, TestReport{
				Verdict:  test.Verdict,
				CheckLog: test.CheckLog,
			})
		}
	}
	return &resp
}

func (v *View) makeSolution(
	c echo.Context, ctx *managers.AccountContext, solution models.Solution, withLogs bool,
) Solution {
	resp := Solution{
		ID:         solution.ID,
		CreateTime: solution.CreateTime,
	}
	if problem, err := v.core.Problems.Get(solution.ProblemID); err == nil {
		problemResp := makeProblem(problem)
		resp.Problem = &problemResp
	}
	if account, err := v.core.Accounts.Get(solution.AuthorID); err == nil {
		switch account.Kind {
		case models.UserAccount:
			if user, err := v.core.Users.GetByAccount(account.ID); err == nil {
				resp.User = &User{ID: user.ID, Login: user.Login}
			}
		}
	}
	resp.Report = v.makeSolutionReport(c, solution, withLogs)
	return resp
}

func (v *View) observeSolutions(c echo.Context) error {
	accountCtx, ok := c.Get(accountCtxKey).(*managers.AccountContext)
	if !ok {
		c.Logger().Error("auth not extracted")
		return fmt.Errorf("auth not extracted")
	}
	var resp Solutions
	solutions, err := v.core.Solutions.All()
	if err != nil {
		c.Logger().Error(err)
		return err
	}
	for _, solution := range solutions {
		permissions := v.getSolutionPermissions(accountCtx, solution)
		if permissions.HasPermission(models.ObserveSolutionRole) {
			resp.Solutions = append(resp.Solutions, v.makeSolution(c, accountCtx, solution, false))
		}
	}
	sort.Sort(solutionSorter(resp.Solutions))
	return c.JSON(http.StatusOK, resp)
}

func (v *View) observeSolution(c echo.Context) error {
	solution, ok := c.Get(solutionKey).(models.Solution)
	if !ok {
		c.Logger().Error("solution not extracted")
		return fmt.Errorf("solution not extracted")
	}
	accountCtx, ok := c.Get(accountCtxKey).(*managers.AccountContext)
	if !ok {
		c.Logger().Error("auth not extracted")
		return fmt.Errorf("auth not extracted")
	}
	return c.JSON(http.StatusOK, v.makeSolution(c, accountCtx, solution, true))
}

func (v *View) extractSolution(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := strconv.ParseInt(c.Param("solution"), 10, 64)
		if err != nil {
			c.Logger().Warn(err)
			resp := errorResponse{Message: "invalid solution ID"}
			return c.JSON(http.StatusBadRequest, resp)
		}
		solution, err := v.core.Solutions.Get(id)
		if err == sql.ErrNoRows {
			if err := v.core.Solutions.Sync(getContext(c)); err != nil {
				return err
			}
			solution, err = v.core.Solutions.Get(id)
		}
		if err != nil {
			if err == sql.ErrNoRows {
				resp := errorResponse{Message: "solution not found"}
				return c.JSON(http.StatusNotFound, resp)
			}
			c.Logger().Error(err)
			return err
		}
		accountCtx, ok := c.Get(accountCtxKey).(*managers.AccountContext)
		if !ok {
			c.Logger().Error("auth not extracted")
			return fmt.Errorf("auth not extracted")
		}
		c.Set(solutionKey, solution)
		c.Set(permissionCtxKey, v.getSolutionPermissions(accountCtx, solution))
		return next(c)
	}
}

func (v *View) getSolutionPermissions(
	ctx *managers.AccountContext, solution models.Solution,
) managers.PermissionSet {
	permissions := ctx.Permissions.Clone()
	if solution.ID == 0 {
		return permissions
	}
	if account := ctx.Account; account != nil &&
		solution.AuthorID != 0 && account.ID == int64(solution.AuthorID) {
		permissions[models.ObserveSolutionRole] = struct{}{}
	}
	return permissions
}
