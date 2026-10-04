package station

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

type Service struct {
	repo  *repository.MemoryRepository
	now   func() time.Time
	seq   atomic.Uint64
	seeds []OperatorAssignment
}

// OperatorAssignment binds one operator name to a responsibility role.
type OperatorAssignment struct {
	Name string
	Role domain.Role
}

// DefaultOperators returns the built-in roster used by the local server and
// the workflow checks. Two distinct preparation leads are seeded so plan
// creation and approval can be exercised as separate people.
func DefaultOperators() []OperatorAssignment {
	return []OperatorAssignment{
		{Name: "registrar-song", Role: domain.RoleRegistrar},
		{Name: "lead-wang", Role: domain.RolePrepLead},
		{Name: "lead-zhao", Role: domain.RolePrepLead},
		{Name: "preparator-chen", Role: domain.RolePreparator},
		{Name: "keeper-zhou", Role: domain.RoleCurator},
	}
}

type Option func(*Service)

// WithOperators seeds the responsibility directory at construction time.
func WithOperators(assignments ...OperatorAssignment) Option {
	return func(s *Service) {
		s.seeds = append(s.seeds, assignments...)
	}
}

func New(repo *repository.MemoryRepository, opts ...Option) *Service {
	if repo == nil {
		repo = repository.NewMemory()
	}
	s := &Service{
		repo: repo,
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	for _, seed := range s.seeds {
		if err := s.assignRole(seed.Name, seed.Role); err != nil {
			panic("station: invalid seeded operator " + seed.Name + ": " + err.Error())
		}
	}
	s.seeds = nil
	return s
}

// AssignRole grants or replaces the responsibility of one operator. The change
// commits through the same repository mutation as business writes, so a role
// change and a concurrent guarded action resolve in a deterministic order.
func (s *Service) AssignRole(ctx context.Context, name string, role domain.Role) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.assignRole(name, role)
}

func (s *Service) assignRole(name string, role domain.Role) error {
	cleaned, err := operatorName("操作人", name)
	if err != nil {
		return err
	}
	if !role.Valid() {
		return contracts.Invalid("unknown_role", "未知职责 "+string(role))
	}
	return s.repo.Mutate(func(m *repository.Mutation) error {
		m.PutOperator(cleaned, role)
		return nil
	})
}

// RevokeRole removes an operator from the responsibility directory. Revoking
// an unknown operator is a no-op so repeated revocations stay deterministic.
func (s *Service) RevokeRole(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cleaned, err := operatorName("操作人", name)
	if err != nil {
		return err
	}
	return s.repo.Mutate(func(m *repository.Mutation) error {
		m.DeleteOperator(cleaned)
		return nil
	})
}

func (s *Service) nextID(prefix string) string {
	var random [6]byte
	if _, err := rand.Read(random[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(random[:])
	}
	local := s.seq.Add(1)
	return prefix + "-" + time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString([]byte{byte(local)})
}

type PlanSnapshot struct {
	Plan     domain.PrepPlan        `json:"plan"`
	Progress []domain.ProgressEntry `json:"progress"`
}

func (s *Service) ListSpecimens(ctx context.Context) ([]domain.Specimen, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.Specimen
	err := s.repo.Read(func(m *repository.Mutation) error {
		values = m.ListSpecimens()
		return nil
	})
	return values, err
}

func (s *Service) GetSpecimen(ctx context.Context, specimenID string) (domain.Specimen, error) {
	if err := ctx.Err(); err != nil {
		return domain.Specimen{}, err
	}
	var value domain.Specimen
	err := s.repo.Read(func(m *repository.Mutation) error {
		found, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}
		value = found
		return nil
	})
	return value, err
}
