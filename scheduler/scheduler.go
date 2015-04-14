package scheduler

import (
	"errors"
	"fmt"

	"github.com/intelsdilabs/pulse/core"
	"github.com/intelsdilabs/pulse/core/cdata"
)

var (
	MetricManagerNotSet = errors.New("MetricManager is not set.")
	SchedulerNotStarted = errors.New("Scheduler is not started.")
)

type schedulerState int

const (
	schedulerStopped schedulerState = iota
	schedulerStarted
)

// managesMetric is implemented by control
// On startup a scheduler will be created and passed a reference to control
type managesMetric interface {
	SubscribeMetricType(mt core.MetricType, cd *cdata.ConfigDataNode) (core.MetricType, []error)
	UnsubscribeMetricType(mt core.MetricType)
}

type scheduler struct {
	workManager   *workManager
	metricManager managesMetric
	tasks         *taskCollection
	state         schedulerState
}

type managesWork interface {
	Work(job) job
}

// New returns an instance of the scheduler
// The MetricManager must be set before the scheduler can be started.
// The MetricManager must be started before it can be used.
func New(poolSize, queueSize int) *scheduler {
	s := &scheduler{
		tasks: newTaskCollection(),
	}

	s.workManager = newWorkManager(int64(queueSize), poolSize)
	s.workManager.Start()

	return s
}

type taskErrors struct {
	errs []error
}

func (t *taskErrors) Errors() []error {
	return t.errs
}

// CreateTask creates and returns task
func (s *scheduler) CreateTask(mts []core.MetricType, sch core.Schedule, cdt *cdata.ConfigDataTree, wf core.Workflow, opts ...core.TaskOption) (core.Task, core.TaskErrors) {
	te := &taskErrors{
		errs: make([]error, 0),
	}

	if s.state != schedulerStarted {
		te.errs = append(te.errs, SchedulerNotStarted)
		return nil, te
	}

	//validate Schedule
	if err := sch.Validate(); err != nil {
		te.errs = append(te.errs, err)
		return nil, te
	}

	//subscribe to MT
	//if we encounter an error we will unwind successful subscriptions
	subscriptions := make([]core.MetricType, 0)
	for _, m := range mts {
		cd := cdt.Get(m.Namespace())
		mt, err := s.metricManager.SubscribeMetricType(m, cd)
		if err == nil {
			//mt := newMetricType(m, config)
			//mtc = append(mtc, mt)
			subscriptions = append(subscriptions, mt)
		} else {
			te.errs = append(te.errs, err...)
		}
	}

	if len(te.errs) > 0 {
		//unwind successful subscriptions
		for _, sub := range subscriptions {
			s.metricManager.UnsubscribeMetricType(sub)
		}
		return nil, te
	}

	sched, err := assertSchedule(sch)
	if err != nil {
		te.errs = append(te.errs, err)
		return nil, te
	}

	workf := newWorkflowFromMap(wf.Map())
	task := newTask(sched, subscriptions, workf, s.workManager, opts...)

	// Add task to taskCollection
	if err := s.tasks.add(task); err != nil {
		te.errs = append(te.errs, err)
		return nil, te
	}

	return task, nil
}

//GetTasks returns a copy of the tasks in a map where the task id is the key
func (s *scheduler) GetTasks() map[uint64]core.Task {
	tasks := make(map[uint64]core.Task)
	for id, t := range s.tasks.Table() {
		tasks[id] = t
	}
	return tasks
}

//GetTask provided the task id a task is returned
func (s *scheduler) GetTask(id uint64) (core.Task, error) {
	task := s.tasks.Get(id)
	if task == nil {
		return nil, errors.New(fmt.Sprintf("No task with Id '%v'", id))
	}
	return task, nil
}

// Start starts the scheduler
func (s *scheduler) Start() error {
	if s.metricManager == nil {
		return MetricManagerNotSet
	}
	s.state = schedulerStarted
	return nil
}

func (s *scheduler) Stop() {
	s.state = schedulerStopped
}

// Set metricManager for scheduler
func (s *scheduler) SetMetricManager(mm managesMetric) {
	s.metricManager = mm
}