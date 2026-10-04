package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

func (s *Service) ReleaseSpecimen(
	ctx context.Context,
	actor contracts.Actor,
	specimenID string,
	request contracts.ReleaseRequest,
) (domain.ReleaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReleaseRecord{}, err
	}
	if err := requireRole(actor, contracts.RoleCurator, "办理研究放行"); err != nil {
		return domain.ReleaseRecord{}, err
	}
	approver, err := recordedName(actor, request.ApprovedBy)
	if err != nil {
		return domain.ReleaseRecord{}, err
	}

	var created domain.ReleaseRecord
	err = s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}
		if m.HasOpenPlan(specimenID) {
			return contracts.Conflict("open_plan_blocks_release", "存在未结束方案，不能办理研究放行")
		}
		if _, exists := m.ReleaseForSpecimen(specimenID); exists {
			return contracts.Conflict("release_exists", "该标本已经存在放行记录")
		}
		record, err := domain.NewReleaseRecord(
			s.nextID("release"),
			specimen,
			request.Purpose,
			request.Recipient,
			approver,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := specimen.MarkAvailable(s.now()); err != nil {
			return err
		}
		m.PutRelease(record)
		m.PutSpecimen(specimen)
		created = record
		return nil
	})
	return created, err
}

// RevokeRelease compensates an existing release: the original record is
// preserved and marked revoked, and the specimen returns to stabilized in the
// same repository mutation. Only a curator may revoke, and only the current
// (non-revoked) record can be revoked.
func (s *Service) RevokeRelease(
	ctx context.Context,
	actor contracts.Actor,
	specimenID string,
	request contracts.RevokeReleaseRequest,
) (domain.ReleaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReleaseRecord{}, err
	}
	if err := requireRole(actor, contracts.RoleCurator, "撤销研究放行"); err != nil {
		return domain.ReleaseRecord{}, err
	}

	var updated domain.ReleaseRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}
		record, exists := m.ReleaseForSpecimen(specimenID)
		if !exists {
			return contracts.NotFound("release_not_found", "该标本不存在放行记录")
		}
		if err := record.Revoke(actor.Name, request.Reason, s.now()); err != nil {
			return err
		}
		if err := specimen.RevokeAvailability(s.now()); err != nil {
			return err
		}
		m.PutRelease(record)
		m.PutSpecimen(specimen)
		updated = record
		return nil
	})
	return updated, err
}
