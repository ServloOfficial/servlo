package cli

import (
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/workerheal"
)

// PruneOrphanedWorkerUnits tears down every unit whose checkout is gone, and
// returns how many it removed. Takes the detected set rather than
// detecting itself so the decision and the teardown stay separable, and so a
// caller that already holds a snapshot does not pay for a second one.
//
// A checkout removed outside servlo leaves a unit pinning WorkingDirectory to a
// directory that no longer exists, so systemd retries it at RestartSec forever
// without the command ever running. There is no state to keep and no
// way for it to succeed again, so it is removed rather than reported.
func PruneOrphanedWorkerUnits(workers []UnhealthyWorker) int {
	pruned := 0
	for _, w := range workers {
		if w.State != workerheal.StateOrphaned {
			continue
		}
		if err := stopWorkerUnit(w.Unit, w.Worker, w.Site+"/"+w.Worker); err != nil {
			feedback.Warn("pruning orphaned worker %s: %v", w.Unit, err)
			continue
		}
		pruned++
	}
	return pruned
}

// PruneOrphanedWorkers detects and prunes in one call, for the daemon's
// startup reconciliation. Errors are swallowed deliberately: a detection that
// cannot read the unit cache is a transient condition, and the next pass picks
// the orphans up again.
func PruneOrphanedWorkers() int {
	workers, err := DetectUnhealthyWorkers()
	if err != nil {
		return 0
	}
	return PruneOrphanedWorkerUnits(workers)
}
