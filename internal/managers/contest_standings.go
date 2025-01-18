package managers

import (
	"database/sql"
	"sort"
	"sync"
	"time"

	"github.com/udovin/solve/internal/core"
	"github.com/udovin/solve/internal/db"
	"github.com/udovin/solve/internal/models"
	"github.com/udovin/solve/internal/perms"
)

type ContestStandingsColumn struct {
	Problem           models.ContestProblem
	TotalSolutions    int
	AcceptedSolutions int
}

type ContestStandingsCell struct {
	Column  int
	Verdict models.Verdict
	Points  float64
	Attempt int
	Time    int64
}

type ContestStandingsRow struct {
	Participant     models.ContestParticipant
	FakeParticipant *models.ContestFakeParticipant
	Cells           []ContestStandingsCell
	Score           float64
	Penalty         *int64
	Place           int
}

type ContestStandings struct {
	Columns []ContestStandingsColumn
	Rows    []ContestStandingsRow
	Stage   ContestStage
	Frozen  bool
}

type ContestStandingsManager struct {
	contestParticipants     *models.ContestParticipantStore
	contestSolutions        *models.ContestSolutionStore
	contestProblems         *models.ContestProblemStore
	contestFakeParticipants *models.ContestFakeParticipantStore
	contestFakeSolutions    *models.ContestFakeSolutionStore
	solutions               *models.SolutionStore
	settings                *models.SettingStore
	cache                   map[standingsCacheKey]*standingsCache
	mutex                   sync.Mutex
}

func NewContestStandingsManager(core *core.Core) *ContestStandingsManager {
	return &ContestStandingsManager{
		contestParticipants:     core.ContestParticipants,
		contestSolutions:        core.ContestSolutions,
		contestProblems:         core.ContestProblems,
		contestFakeParticipants: core.ContestFakeParticipants,
		contestFakeSolutions:    core.ContestFakeSolutions,
		settings:                core.Settings,
		solutions:               core.Solutions,
		cache:                   map[standingsCacheKey]*standingsCache{},
	}
}

type BuildStandingsOptions struct {
	OnlyOfficial bool
	IgnoreFreeze bool
}

func (m *ContestStandingsManager) BuildStandings(
	ctx *ContestContext, options BuildStandingsOptions,
) (*ContestStandings, error) {
	useCache, err := m.settings.GetBool("standings.use_cache")
	if err != nil || !useCache.OrElse(true) {
		return m.buildStandings(ctx, options)
	}
	key := standingsCacheKey{
		ContestID:     ctx.Contest.ID,
		OnlyOfficial:  options.OnlyOfficial,
		IgnoreFreeze:  options.IgnoreFreeze,
		FullStandings: ctx.HasPermission(perms.ObserveContestFullStandingsRole),
	}
	m.mutex.Lock()
	cache, ok := m.cache[key]
	if ok {
		select {
		case <-cache.Done:
			if cache.Error == nil && time.Since(cache.Time) < 15*time.Second {
				m.mutex.Unlock()
				return cache.Standings, nil
			}
		default:
			m.mutex.Unlock()
			<-cache.Done
			return cache.Standings, cache.Error
		}
	}
	done := make(chan struct{})
	defer close(done)
	cache = &standingsCache{Done: done, Time: ctx.Now}
	m.cache[key] = cache
	m.mutex.Unlock()
	cache.Standings, cache.Error = m.buildStandings(ctx, options)
	return cache.Standings, cache.Error
}

type standingsCache struct {
	Done      <-chan struct{}
	Time      time.Time
	Standings *ContestStandings
	Error     error
}

type standingsCacheKey struct {
	ContestID     int64
	OnlyOfficial  bool
	IgnoreFreeze  bool
	FullStandings bool
}

func (m *ContestStandingsManager) buildStandings(
	ctx *ContestContext, options BuildStandingsOptions,
) (*ContestStandings, error) {
	participantRows, err := m.contestParticipants.FindByContest(ctx, ctx.Contest.ID)
	if err != nil {
		return nil, err
	}
	participants, err := db.CollectRows(participantRows)
	if err != nil {
		return nil, err
	}
	contestProblemRows, err := m.contestProblems.FindByContest(ctx, ctx.Contest.ID)
	if err != nil {
		return nil, err
	}
	contestProblems, err := db.CollectRows(contestProblemRows)
	if err != nil {
		return nil, err
	}
	fakeParticipantRows, err := m.contestFakeParticipants.FindByContest(ctx, ctx.Contest.ID)
	if err != nil {
		return nil, err
	}
	fakeParticipants, err := db.CollectRows(fakeParticipantRows)
	if err != nil {
		return nil, err
	}
	sortFunc(contestProblems, func(lhs, rhs models.ContestProblem) bool {
		return lhs.Code < rhs.Code
	})
	solutionsByParticipant := map[int64][]models.ContestSolution{}
	if err := func() error {
		solutions, err := m.contestSolutions.FindByContest(ctx, ctx.Contest.ID)
		if err != nil {
			return err
		}
		defer func() { _ = solutions.Close() }()
		for solutions.Next() {
			solution := solutions.Row()
			solutionsByParticipant[solution.ParticipantID] = append(
				solutionsByParticipant[solution.ParticipantID], solution,
			)
		}
		return solutions.Err()
	}(); err != nil {
		return nil, err
	}
	fakeSolutionsByParticipant := map[int64][]models.ContestFakeSolution{}
	if err := func() error {
		solutions, err := m.contestFakeSolutions.FindByContest(ctx, ctx.Contest.ID)
		if err != nil {
			return err
		}
		defer func() { _ = solutions.Close() }()
		for solutions.Next() {
			solution := solutions.Row()
			fakeSolutionsByParticipant[solution.ParticipantID] = append(
				fakeSolutionsByParticipant[solution.ParticipantID], solution,
			)
		}
		return solutions.Err()
	}(); err != nil {
		return nil, err
	}
	switch ctx.ContestConfig.StandingsKind {
	case models.IOIStandings:
		return m.buildIOIStandings(
			ctx, options, contestProblems,
			participants, solutionsByParticipant,
			fakeParticipants, fakeSolutionsByParticipant,
		)
	default:
		return m.buildICPCStandings(
			ctx, options, contestProblems,
			participants, solutionsByParticipant,
			fakeParticipants, fakeSolutionsByParticipant,
		)
	}
}

func (m *ContestStandingsManager) buildICPCStandings(
	ctx *ContestContext,
	options BuildStandingsOptions,
	contestProblems []models.ContestProblem,
	participants []models.ContestParticipant,
	solutionsByParticipant map[int64][]models.ContestSolution,
	fakeParticipants []models.ContestFakeParticipant,
	fakeSolutionsByParticipant map[int64][]models.ContestFakeSolution,
) (*ContestStandings, error) {
	standings := ContestStandings{}
	columnByProblem := map[int64]int{}
	for i, problem := range contestProblems {
		standings.Columns = append(standings.Columns, ContestStandingsColumn{
			Problem: problem,
		})
		columnByProblem[problem.ID] = i
	}
	observeFullStandings := ctx.HasPermission(perms.ObserveContestFullStandingsRole)
	ignoreFreeze := options.IgnoreFreeze && observeFullStandings
	contestTime := ctx.GetEffectiveContestTime()
	standings.Stage = contestTime.Stage()
	// contestTime will be invalid when standings.Stage != ContestStarted. We consider this normal.
	standings.Frozen = !ignoreFreeze && isVerdictFrozen(ctx, standings.Stage, int64(contestTime))
	for _, participant := range participants {
		if options.OnlyOfficial && participant.Kind != models.RegularParticipant {
			continue
		}
		if !observeFullStandings {
			switch participant.Kind {
			case models.RegularParticipant:
			case models.UpsolvingParticipant:
				if standings.Stage != ContestFinished {
					continue
				}
			case models.VirtualParticipant:
			default:
				continue
			}
		}
		beginTime := getParticipantBeginTime(&ctx.ContestConfig, &participant)
		participantSolutions, ok := solutionsByParticipant[participant.ID]
		if !ok {
			continue
		}
		solutionsByColumn := map[int][]models.Solution{}
		for _, participantSolution := range participantSolutions {
			solution, err := m.solutions.Get(ctx, participantSolution.ID)
			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				return nil, err
			}
			column, ok := columnByProblem[participantSolution.ProblemID]
			if !ok {
				continue
			}
			solutionsByColumn[column] = append(solutionsByColumn[column], solution)
		}
		row := ContestStandingsRow{
			Participant: participant,
		}
		for i := range standings.Columns {
			solutions, ok := solutionsByColumn[i]
			if !ok {
				continue
			}
			sortFunc(solutions, func(lhs, rhs models.Solution) bool {
				if lhs.CreateTime != rhs.CreateTime {
					return lhs.CreateTime < rhs.CreateTime
				}
				return lhs.ID < rhs.ID
			})
			cell := ContestStandingsCell{
				Column: i,
			}
			for _, solution := range solutions {
				if solution.CreateTime >= ctx.Now.Unix() {
					continue
				}
				report, err := solution.GetReport()
				if err != nil {
					continue
				}
				if report == nil {
					cell.Attempt++
					cell.Verdict = 0
					break
				}
				if report.Verdict == models.CompilationError {
					continue
				}
				cell.Attempt++
				if beginTime != 0 {
					cell.Time = solution.CreateTime - beginTime
					if cell.Time < 0 {
						cell.Time = 0
					}
				}
				cell.Verdict = report.Verdict
				if !ignoreFreeze && isVerdictFrozen(ctx, standings.Stage, cell.Time) {
					cell.Verdict = 0
				}
				if report.Verdict == models.Accepted {
					break
				}
			}
			if cell.Attempt > 0 {
				row.Cells = append(row.Cells, cell)
			}
		}
		var penalty int64
		for _, cell := range row.Cells {
			column := &standings.Columns[cell.Column]
			column.TotalSolutions += cell.Attempt
			if cell.Verdict == models.Accepted {
				row.Score += getProblemScore(column.Problem)
				penalty += int64(cell.Attempt-1)*20 + cell.Time/60
				column.AcceptedSolutions++
			}
		}
		if participant.Kind == models.RegularParticipant ||
			participant.Kind == models.VirtualParticipant {
			row.Penalty = &penalty
		}
		standings.Rows = append(standings.Rows, row)
	}
	for _, participant := range fakeParticipants {
		participantSolutions, ok := fakeSolutionsByParticipant[participant.ID]
		if !ok {
			continue
		}
		solutionsByColumn := map[int][]models.ContestFakeSolution{}
		for _, participantSolution := range participantSolutions {
			column, ok := columnByProblem[participantSolution.ProblemID]
			if !ok {
				continue
			}
			solutionsByColumn[column] = append(solutionsByColumn[column], participantSolution)
		}
		row := ContestStandingsRow{
			FakeParticipant: &participant,
		}
		for i := range standings.Columns {
			solutions, ok := solutionsByColumn[i]
			if !ok {
				continue
			}
			sortFunc(solutions, func(lhs, rhs models.ContestFakeSolution) bool {
				if lhs.ContestTime != rhs.ContestTime {
					return lhs.ContestTime < rhs.ContestTime
				}
				return lhs.ID < rhs.ID
			})
			cell := ContestStandingsCell{
				Column: i,
			}
			for _, solution := range solutions {
				if contestTime.Before(solution.ContestTime) {
					continue
				}
				report, err := solution.GetReport()
				if err != nil {
					continue
				}
				if report == nil {
					cell.Attempt++
					cell.Verdict = 0
					break
				}
				if report.Verdict == models.CompilationError {
					continue
				}
				cell.Attempt++
				cell.Time = solution.ContestTime
				cell.Verdict = report.Verdict
				if !ignoreFreeze && isVerdictFrozen(ctx, standings.Stage, cell.Time) {
					cell.Verdict = 0
				}
				if report.Verdict == models.Accepted {
					break
				}
			}
			if cell.Attempt > 0 {
				row.Cells = append(row.Cells, cell)
			}
		}
		var penalty int64
		for _, cell := range row.Cells {
			column := &standings.Columns[cell.Column]
			column.TotalSolutions += cell.Attempt
			if cell.Verdict == models.Accepted {
				row.Score += getProblemScore(column.Problem)
				penalty += int64(cell.Attempt-1)*20 + cell.Time/60
				column.AcceptedSolutions++
			}
		}
		row.Penalty = &penalty
		standings.Rows = append(standings.Rows, row)
	}
	sortFunc(standings.Rows, stableParticipantLess)
	calculatePlaces(standings.Rows)
	return &standings, nil
}

func (m *ContestStandingsManager) buildIOIStandings(
	ctx *ContestContext,
	options BuildStandingsOptions,
	contestProblems []models.ContestProblem,
	participants []models.ContestParticipant,
	solutionsByParticipant map[int64][]models.ContestSolution,
	fakeParticipants []models.ContestFakeParticipant,
	fakeSolutionsByParticipant map[int64][]models.ContestFakeSolution,
) (*ContestStandings, error) {
	standings := ContestStandings{}
	columnByProblem := map[int64]int{}
	for i, problem := range contestProblems {
		standings.Columns = append(standings.Columns, ContestStandingsColumn{
			Problem: problem,
		})
		columnByProblem[problem.ID] = i
	}
	observeFullStandings := ctx.HasPermission(perms.ObserveContestFullStandingsRole)
	ignoreFreeze := options.IgnoreFreeze && observeFullStandings
	contestTime := ctx.GetEffectiveContestTime()
	standings.Stage = contestTime.Stage()
	// contestTime will be invalid when standings.Stage != ContestStarted. We consider this normal.
	standings.Frozen = !ignoreFreeze && isVerdictFrozen(ctx, standings.Stage, int64(contestTime))
	for _, participant := range participants {
		if options.OnlyOfficial && participant.Kind != models.RegularParticipant {
			continue
		}
		if !observeFullStandings {
			switch participant.Kind {
			case models.RegularParticipant:
			case models.UpsolvingParticipant:
				if standings.Stage != ContestFinished {
					continue
				}
			case models.VirtualParticipant:
			default:
				continue
			}
		}
		beginTime := getParticipantBeginTime(&ctx.ContestConfig, &participant)
		participantSolutions, ok := solutionsByParticipant[participant.ID]
		if !ok {
			continue
		}
		solutionsByColumn := map[int][]models.Solution{}
		for _, participantSolution := range participantSolutions {
			solution, err := m.solutions.Get(ctx, participantSolution.ID)
			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				return nil, err
			}
			column, ok := columnByProblem[participantSolution.ProblemID]
			if !ok {
				continue
			}
			solutionsByColumn[column] = append(solutionsByColumn[column], solution)
		}
		row := ContestStandingsRow{
			Participant: participant,
		}
		for i := range standings.Columns {
			solutions, ok := solutionsByColumn[i]
			if !ok {
				continue
			}
			sortFunc(solutions, func(lhs, rhs models.Solution) bool {
				if lhs.CreateTime != rhs.CreateTime {
					return lhs.CreateTime < rhs.CreateTime
				}
				return lhs.ID < rhs.ID
			})
			cell := ContestStandingsCell{
				Column: i,
			}
			for _, solution := range solutions {
				if solution.CreateTime >= ctx.Now.Unix() {
					continue
				}
				report, err := solution.GetReport()
				if err != nil {
					continue
				}
				if report == nil {
					cell.Attempt++
					cell.Verdict = 0
					break
				}
				if report.Verdict == models.CompilationError {
					continue
				}
				cell.Attempt++
				if beginTime != 0 {
					cell.Time = solution.CreateTime - beginTime
					if cell.Time < 0 {
						cell.Time = 0
					}
				}
				if !ignoreFreeze && isVerdictFrozen(ctx, standings.Stage, cell.Time) {
					cell.Verdict = 0
				} else {
					if cell.Verdict == 0 {
						cell.Verdict = report.Verdict
					}
					if report.Points != nil && cell.Points < *report.Points {
						cell.Verdict = report.Verdict
						cell.Points = *report.Points
					}
				}
			}
			if cell.Attempt > 0 {
				row.Cells = append(row.Cells, cell)
			}
		}
		for _, cell := range row.Cells {
			column := &standings.Columns[cell.Column]
			column.TotalSolutions += cell.Attempt
			row.Score += cell.Points
			if cell.Verdict == models.Accepted {
				column.AcceptedSolutions++
			}
		}
		standings.Rows = append(standings.Rows, row)
	}
	for _, participant := range fakeParticipants {
		participantSolutions, ok := fakeSolutionsByParticipant[participant.ID]
		if !ok {
			continue
		}
		solutionsByColumn := map[int][]models.ContestFakeSolution{}
		for _, participantSolution := range participantSolutions {
			column, ok := columnByProblem[participantSolution.ProblemID]
			if !ok {
				continue
			}
			solutionsByColumn[column] = append(solutionsByColumn[column], participantSolution)
		}
		row := ContestStandingsRow{
			FakeParticipant: &participant,
		}
		for i := range standings.Columns {
			solutions, ok := solutionsByColumn[i]
			if !ok {
				continue
			}
			sortFunc(solutions, func(lhs, rhs models.ContestFakeSolution) bool {
				if lhs.ContestTime != rhs.ContestTime {
					return lhs.ContestTime < rhs.ContestTime
				}
				return lhs.ID < rhs.ID
			})
			cell := ContestStandingsCell{
				Column: i,
			}
			for _, solution := range solutions {
				if contestTime.Before(solution.ContestTime) {
					continue
				}
				report, err := solution.GetReport()
				if err != nil {
					continue
				}
				if report == nil {
					cell.Attempt++
					cell.Verdict = 0
					break
				}
				if report.Verdict == models.CompilationError {
					continue
				}
				cell.Attempt++
				cell.Time = solution.ContestTime
				if !ignoreFreeze && isVerdictFrozen(ctx, standings.Stage, cell.Time) {
					cell.Verdict = 0
				} else {
					if cell.Verdict == 0 {
						cell.Verdict = report.Verdict
					}
					if report.Points != nil && cell.Points < *report.Points {
						cell.Verdict = report.Verdict
						cell.Points = *report.Points
					}
				}
			}
			if cell.Attempt > 0 {
				row.Cells = append(row.Cells, cell)
			}
		}
		for _, cell := range row.Cells {
			column := &standings.Columns[cell.Column]
			column.TotalSolutions += cell.Attempt
			row.Score += cell.Points
			if cell.Verdict == models.Accepted {
				column.AcceptedSolutions++
			}
		}
		standings.Rows = append(standings.Rows, row)
	}
	sortFunc(standings.Rows, stableParticipantLess)
	calculatePlaces(standings.Rows)
	return &standings, nil
}

func calculatePlaces(rows []ContestStandingsRow) {
	it := -1
	place := 1
	for i := range rows {
		if rows[i].Participant.Kind == models.RegularParticipant {
			rows[i].Place = place
			place++
			if it >= 0 && !participantLess(rows[it], rows[i]) {
				rows[i].Place = rows[it].Place
			}
			it = i
		}
	}
}

// time can be less than zero for stage != ContestStarted.
func isVerdictFrozen(
	ctx *ContestContext, stage ContestStage, verdictTime int64,
) bool {
	if ctx.ContestConfig.FreezeBeginDuration == 0 {
		return false
	}
	if stage == ContestStarted {
		return verdictTime >= int64(ctx.ContestConfig.FreezeBeginDuration)
	}
	if stage == ContestFinished {
		return ctx.ContestConfig.FreezeEndTime == 0 ||
			ctx.Now.Unix() < int64(ctx.ContestConfig.FreezeEndTime)
	}
	return false
}

func getParticipantOrder(kind models.ParticipantKind) int {
	switch kind {
	case models.ManagerParticipant:
		return 0
	case models.RegularParticipant, models.VirtualParticipant:
		return 1
	default:
		return 2
	}
}

func stableParticipantLess(lhs, rhs ContestStandingsRow) bool {
	lhsOrder := getParticipantOrder(lhs.Participant.Kind)
	rhsOrder := getParticipantOrder(rhs.Participant.Kind)
	if lhsOrder != rhsOrder {
		return lhsOrder < rhsOrder
	}
	if lhs.Score != rhs.Score {
		return lhs.Score > rhs.Score
	}
	if lhs.Penalty != nil && rhs.Penalty != nil && *lhs.Penalty != *rhs.Penalty {
		return *lhs.Penalty < *rhs.Penalty
	}
	return lhs.Participant.ID < rhs.Participant.ID
}

func participantLess(lhs, rhs ContestStandingsRow) bool {
	lhsOrder := getParticipantOrder(lhs.Participant.Kind)
	rhsOrder := getParticipantOrder(rhs.Participant.Kind)
	if lhsOrder != rhsOrder {
		return lhsOrder < rhsOrder
	}
	if lhs.Score != rhs.Score {
		return lhs.Score > rhs.Score
	}
	if lhs.Penalty != nil && rhs.Penalty != nil {
		return *lhs.Penalty < *rhs.Penalty
	}
	return false
}

func getProblemScore(problem models.ContestProblem) float64 {
	config, err := problem.GetConfig()
	if err != nil {
		return 1
	}
	if config.Points != nil {
		return float64(*config.Points)
	}
	return 1
}

func sortFunc[T any](a []T, less func(T, T) bool) {
	impl := sortFuncImpl[T]{data: a, less: less}
	sort.Sort(&impl)
}

type sortFuncImpl[T any] struct {
	data []T
	less func(T, T) bool
}

func (s *sortFuncImpl[T]) Len() int {
	return len(s.data)
}

func (s *sortFuncImpl[T]) Swap(i, j int) {
	s.data[i], s.data[j] = s.data[j], s.data[i]
}

func (s *sortFuncImpl[T]) Less(i, j int) bool {
	return s.less(s.data[i], s.data[j])
}
