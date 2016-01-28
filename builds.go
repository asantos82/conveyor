package conveyor

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ErrDuplicateBuild can be returned when we try to start a build for a sha that
// is already in a "pending" or "building" state. We want to ensure that we only
// have 1 concurrent build for a given sha.
//
// This is also enforced at the db level with the `unique_build` constraint.
var ErrDuplicateBuild = errors.New("a build for this sha is already pending or building")

// The database constraint that counts as an ErrDuplicateBuild.
const uniqueBuildConstraint = "unique_build"

// Build represents a build of a commit.
type Build struct {
	// A unique identifier for this build.
	ID string `db:"id"`
	// The repository that this build relates to.
	Repository string `db:"repository"`
	// The branch that this build relates to.
	Branch string `db:"branch"`
	// The sha that this build relates to.
	Sha string `db:"sha"`
	// The current state of the build.
	State BuildState `db:"state"`
	// The time that this build was created.
	CreatedAt time.Time `db:"created_at"`
	// The time that the build was started.
	StartedAt *time.Time `db:"started_at"`
	// The time that the build was completed.
	CompletedAt *time.Time `db:"completed_at"`
}

type BuildState int

const (
	StatePending BuildState = iota
	StateBuilding
	StateFailed
	StateSucceeded
)

func (s BuildState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateBuilding:
		return "building"
	case StateFailed:
		return "failed"
	case StateSucceeded:
		return "succeeded"
	default:
		panic(fmt.Sprintf("unknown build state: %v", s))
	}
}

// Scan implements the sql.Scanner interface.
func (s *BuildState) Scan(src interface{}) error {
	if v, ok := src.([]byte); ok {
		switch string(v) {
		case "pending":
			*s = StatePending
		case "building":
			*s = StateBuilding
		case "failed":
			*s = StateFailed
		case "succeeded":
			*s = StateSucceeded
		default:
			return fmt.Errorf("unknown build state: %v", string(v))
		}
	}

	return nil
}

// Value implements the driver.Value interface.
func (s BuildState) Value() (driver.Value, error) {
	return driver.Value(s.String()), nil
}

// buildsCreate inserts a new build into the database.
func buildsCreate(tx *sqlx.Tx, b *Build) error {
	const createBuildSql = `INSERT INTO builds (repository, branch, sha, state) VALUES (:repository, :branch, :sha, :state) RETURNING id`
	err := insert(tx, createBuildSql, b, &b.ID)
	if err, ok := err.(*pq.Error); ok {
		if err.Constraint == uniqueBuildConstraint {
			return ErrDuplicateBuild
		}
	}
	return err
}

// buildsFind finds a build by ID.
func buildsFind(tx *sqlx.Tx, buildID string) (*Build, error) {
	const findBuildSql = `SELECT * FROM builds where id = ?`
	var b Build
	err := tx.Get(&b, tx.Rebind(findBuildSql), buildID)
	return &b, err
}

// buildsUpdateState changes the state of a build.
func buildsUpdateState(tx *sqlx.Tx, buildID string, state BuildState) error {
	var sql string
	switch state {
	case StateBuilding:
		sql = `UPDATE builds SET state = ?, started_at = ? WHERE id = ?`
	case StateSucceeded, StateFailed:
		sql = `UPDATE builds SET state = ?, completed_at = ? WHERE id = ?`
	default:
		panic(fmt.Sprintf("not implemented for %s", state))
	}

	_, err := tx.Exec(tx.Rebind(sql), state, time.Now(), buildID)
	return err
}
