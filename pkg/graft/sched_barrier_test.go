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
		rng := rand.New(rand.NewSource(seed))
		full := randomSchedTasks(rng)
		rng = rand.New(rand.NewSource(seed))
		lean := randomSchedTasks(rng)

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
