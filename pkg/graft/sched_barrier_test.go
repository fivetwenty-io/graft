package graft

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/fivetwenty-io/graft/internal/parallel"
)

// TestSchedulerBarrierMembersKeepWaves checks that naming only the
// latest member of each soft barrier puts every task in the same wave
// as naming every member that comes before it, over graphs of many
// shapes.
func TestSchedulerBarrierMembersKeepWaves(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		full := randomSchedTasks(seededRand(seed))
		lean := randomSchedTasks(seededRand(seed))

		place := make(map[string]int, len(full))
		for i, task := range full {
			place[task.id] = i
		}
		for i, task := range full {
			task.addEveryKeptMember(i, place)
		}
		memberOf := make(map[string][]*schedBarrier)
		seen := make(map[*schedBarrier]bool)
		for _, task := range lean {
			for _, bar := range task.soft {
				if !seen[bar] {
					seen[bar] = true
					for _, id := range bar.members {
						memberOf[id] = append(memberOf[id], bar)
					}
				}
			}
		}
		finishSchedTasks(lean, place, memberOf)

		want, got := schedWaves(t, full), schedWaves(t, lean)
		for id, w := range want {
			if got[id] != w {
				t.Fatalf("seed %d: %s is in wave %d, want %d", seed, id, got[id], w)
			}
		}
	}
}

// TestSchedulerBarrierFallbackNamesEveryKeptMember checks the branch
// finishSchedTasks takes when some hard dependency comes after its task,
// where the waves can't be worked out in one walk. Each waiter must then
// name every member of its barriers that comes before it, each once and
// after its hard dependencies, rather than only the latest one.
func TestSchedulerBarrierFallbackNamesEveryKeptMember(t *testing.T) {
	a := &schedBarrier{members: []string{"t0", "t2", "t3", "t1"}}
	b := &schedBarrier{members: []string{"t1", "t0"}}
	tasks := []*schedTask{
		{id: "t0", deps: []string{"t3"}, soft: []*schedBarrier{b}},
		{id: "t1"},
		{id: "t2", deps: []string{"t1"}, soft: []*schedBarrier{a, b}},
		{id: "t3", soft: []*schedBarrier{a}},
	}
	place := map[string]int{"t0": 0, "t1": 1, "t2": 2, "t3": 3}
	memberOf := map[string][]*schedBarrier{
		"t0": {a, b}, "t1": {a, b}, "t2": {a}, "t3": {a},
	}

	got := finishSchedTasks(tasks, place, memberOf)

	want := map[string][]string{
		"t0": {"t3"},
		"t1": nil,
		"t2": {"t1", "t0"},
		"t3": {"t0", "t2", "t1"},
	}
	if len(got) != len(tasks) {
		t.Fatalf("got %d tasks, want %d", len(got), len(tasks))
	}
	for _, task := range got {
		if fmt.Sprint(task.deps) != fmt.Sprint(want[task.id]) {
			t.Errorf("%s depends on %v, want %v", task.id, task.deps, want[task.id])
		}
	}
	if a.best != "" || b.best != "" {
		t.Errorf("the barriers track a latest member (%q, %q), so the one-walk branch ran", a.best, b.best)
	}
}

// seededRand returns a generator that gives the same graphs for the
// same seed.
func seededRand(seed int64) *rand.Rand {
	//nolint:gosec // G404: the graphs must repeat for a seed, and nothing here is secret
	return rand.New(rand.NewSource(seed))
}

// randomSchedTasks returns tasks in order, each with hard dependencies
// on tasks before it and soft barriers whose members are anywhere.
func randomSchedTasks(rng *rand.Rand) []*schedTask {
	n := 5 + rng.Intn(40)
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%d", i)
	}
	barriers := make([]*schedBarrier, 1+rng.Intn(5))
	for b := range barriers {
		bar := &schedBarrier{}
		for _, id := range ids {
			if rng.Intn(3) == 0 {
				bar.members = append(bar.members, id)
			}
		}
		barriers[b] = bar
	}
	tasks := make([]*schedTask, n)
	for i, id := range ids {
		task := &schedTask{id: id}
		for j := 0; j < i; j++ {
			if rng.Intn(8) == 0 {
				task.deps = append(task.deps, ids[j])
			}
		}
		for _, bar := range barriers {
			if rng.Intn(3) == 0 {
				task.soft = append(task.soft, bar)
			}
		}
		tasks[i] = task
	}
	return tasks
}

// schedWaves schedules tasks and returns each task's wave.
func schedWaves(t *testing.T, tasks []*schedTask) map[string]int {
	t.Helper()
	s := parallel.NewScheduler()
	for _, task := range tasks {
		if err := s.AddTask(&parallel.Task{ID: task.id, Dependencies: task.deps}); err != nil {
			t.Fatal(err)
		}
	}
	waves, err := s.Schedule()
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]int)
	for w, wave := range waves {
		for _, task := range wave {
			out[task.ID] = w
		}
	}
	return out
}
