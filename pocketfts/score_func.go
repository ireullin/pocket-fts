package pocketfts

import (
	"database/sql/driver"
	"fmt"
	"sync"
	"sync/atomic"

	"modernc.org/sqlite"
)

// scoreFunc is the SQL function fetchRecords uses to rank rows by relevance:
// pfts_score(handle, primary_key) returns the row's full-text score, or NULL
// when the row has none. The scores live on the Go side, registered under a
// per-query handle, so SQLite can sort and page by relevance without the
// scores ever becoming a table.
const scoreFunc = "pfts_score"

var (
	scoreSets    sync.Map // int64 handle -> ScoreMap
	nextScoreSet atomic.Int64
)

func init() {
	// Registered once, before any connection opens: modernc makes a
	// registered function available to connections opened afterwards.
	sqlite.MustRegisterScalarFunction(scoreFunc, 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		handle, ok := args[0].(int64)
		if !ok {
			return nil, fmt.Errorf("%s: handle must be an integer, got %T", scoreFunc, args[0])
		}
		scores, ok := scoreSets.Load(handle)
		if !ok {
			return nil, fmt.Errorf("%s: unknown score set %d", scoreFunc, handle)
		}
		if args[1] == nil {
			return nil, nil
		}
		score, ok := scores.(ScoreMap)[fmt.Sprintf("%v", args[1])]
		if !ok {
			return nil, nil
		}
		return score, nil
	})
}

// registerScores makes scores visible to pfts_score under a new handle. The
// returned func removes them; call it once the query has finished reading.
func registerScores(scores ScoreMap) (int64, func()) {
	handle := nextScoreSet.Add(1)
	scoreSets.Store(handle, scores)
	return handle, func() { scoreSets.Delete(handle) }
}
