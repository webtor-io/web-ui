package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// --- Mock implementations ---

type mockReaperStore struct {
	expiredResources    []vaultModels.Resource
	expiredResourcesErr error
	pledgesWithUsers    map[string][]vaultModels.Pledge
	pledgesWithUsersErr map[string]error
}

func (m *mockReaperStore) GetExpiredResources(_ context.Context, _ time.Duration, _ time.Duration) ([]vaultModels.Resource, error) {
	return m.expiredResources, m.expiredResourcesErr
}

func (m *mockReaperStore) GetResourcePledgesWithUsers(_ context.Context, resourceID string) ([]vaultModels.Pledge, error) {
	if m.pledgesWithUsersErr != nil {
		if err, ok := m.pledgesWithUsersErr[resourceID]; ok {
			return nil, err
		}
	}
	if m.pledgesWithUsers != nil {
		return m.pledgesWithUsers[resourceID], nil
	}
	return nil, nil
}

type removePledgeCall struct {
	pledgeID   uuid.UUID
	resourceID string
}

type mockReaperVault struct {
	removePledgeErr    error
	removePledgeCalls  []removePledgeCall
	removeResourceErr  error
	removeResourceIDs  []string
	removePledgeErrMap map[uuid.UUID]error
}

func (m *mockReaperVault) RemovePledge(_ context.Context, pledge *vaultModels.Pledge) error {
	m.removePledgeCalls = append(m.removePledgeCalls, removePledgeCall{
		pledgeID:   pledge.PledgeID,
		resourceID: pledge.ResourceID,
	})
	if m.removePledgeErrMap != nil {
		if err, ok := m.removePledgeErrMap[pledge.PledgeID]; ok {
			return err
		}
	}
	return m.removePledgeErr
}

func (m *mockReaperVault) RemoveResource(_ context.Context, resourceID string) error {
	m.removeResourceIDs = append(m.removeResourceIDs, resourceID)
	return m.removeResourceErr
}

type notificationCall struct {
	email      string
	resourceID string
	action     string
}

type mockReaperNotification struct {
	sendTransferTimeoutErr error
	sendExpiredErr         error
	calls                  []notificationCall
}

func (m *mockReaperNotification) SendTransferTimeout(to string, r *vaultModels.Resource) error {
	m.calls = append(m.calls, notificationCall{
		email:      to,
		resourceID: r.ResourceID,
		action:     "transfer_timeout",
	})
	return m.sendTransferTimeoutErr
}

func (m *mockReaperNotification) SendExpired(to string, r *vaultModels.Resource) error {
	m.calls = append(m.calls, notificationCall{
		email:      to,
		resourceID: r.ResourceID,
		action:     "expired",
	})
	return m.sendExpiredErr
}

// --- Test helpers ---

func newTestReaper(store reaperStore, v reaperVault, n reaperNotification) *reaper {
	return &reaper{
		store:                 store,
		vault:                 v,
		notification:          n,
		expirePeriod:          7 * 24 * time.Hour,
		transferTimeoutPeriod: 7 * 24 * time.Hour,
	}
}

func makeResource(id string, expiredAt *time.Time) vaultModels.Resource {
	return vaultModels.Resource{
		ResourceID: id,
		RequiredVP: 1.0,
		FundedVP:   1.0,
		Name:       "Test Resource " + id,
		ExpiredAt:  expiredAt,
	}
}

func makePledge(pledgeID uuid.UUID, resourceID string, userID uuid.UUID, amount float64, user *models.User) vaultModels.Pledge {
	return vaultModels.Pledge{
		PledgeID:   pledgeID,
		ResourceID: resourceID,
		UserID:     userID,
		Amount:     amount,
		Funded:     true,
		FrozenAt:   time.Now(),
		User:       user,
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// --- Tests for run ---

func TestRun_NoExpiredResources(t *testing.T) {
	store := &mockReaperStore{expiredResources: []vaultModels.Resource{}}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.run(context.Background())

	if len(v.removeResourceIDs) != 0 {
		t.Errorf("expected no resources removed, got %d", len(v.removeResourceIDs))
	}
}

func TestRun_GetExpiredResourcesError(t *testing.T) {
	store := &mockReaperStore{expiredResourcesErr: fmt.Errorf("db error")}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.run(context.Background())

	if len(v.removeResourceIDs) != 0 {
		t.Errorf("expected no resources removed on error, got %d", len(v.removeResourceIDs))
	}
}

func TestRun_MultipleResources(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	store := &mockReaperStore{
		expiredResources: []vaultModels.Resource{
			makeResource("res1", expired),
			makeResource("res2", expired),
		},
		pledgesWithUsers: map[string][]vaultModels.Pledge{},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.run(context.Background())

	if len(v.removeResourceIDs) != 2 {
		t.Errorf("expected 2 resources removed, got %d", len(v.removeResourceIDs))
	}
}

// --- Tests for processResource ---

func TestProcessResource_Expired(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	userID := uuid.NewV4()
	pledgeID := uuid.NewV4()

	store := &mockReaperStore{
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {
				makePledge(pledgeID, "res1", userID, 1.0, &models.User{
					UserID: userID,
					Email:  "user@example.com",
				}),
			},
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	// Should remove pledge
	if len(v.removePledgeCalls) != 1 {
		t.Fatalf("expected 1 pledge removed, got %d", len(v.removePledgeCalls))
	}
	if v.removePledgeCalls[0].pledgeID != pledgeID {
		t.Errorf("expected pledge ID %s, got %s", pledgeID, v.removePledgeCalls[0].pledgeID)
	}

	// Should send expiration notification (not transfer timeout)
	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].action != "expired" {
		t.Errorf("expected 'expired' action, got %q", n.calls[0].action)
	}
	if n.calls[0].email != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %q", n.calls[0].email)
	}

	// Should remove resource
	if len(v.removeResourceIDs) != 1 {
		t.Fatalf("expected 1 resource removed, got %d", len(v.removeResourceIDs))
	}
	if v.removeResourceIDs[0] != "res1" {
		t.Errorf("expected resource ID 'res1', got %q", v.removeResourceIDs[0])
	}
}

func TestProcessResource_TransferTimeout(t *testing.T) {
	// ExpiredAt == nil indicates transfer timeout
	resource := makeResource("res1", nil)
	userID := uuid.NewV4()
	pledgeID := uuid.NewV4()

	store := &mockReaperStore{
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {
				makePledge(pledgeID, "res1", userID, 1.0, &models.User{
					UserID: userID,
					Email:  "user@example.com",
				}),
			},
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	// Should send transfer timeout notification
	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].action != "transfer_timeout" {
		t.Errorf("expected 'transfer_timeout' action, got %q", n.calls[0].action)
	}
}

func TestProcessResource_NoPledges(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)

	store := &mockReaperStore{
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {},
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	// No pledges to remove
	if len(v.removePledgeCalls) != 0 {
		t.Errorf("expected no pledges removed, got %d", len(v.removePledgeCalls))
	}

	// No notifications
	if len(n.calls) != 0 {
		t.Errorf("expected no notifications, got %d", len(n.calls))
	}

	// Resource should still be removed
	if len(v.removeResourceIDs) != 1 {
		t.Fatalf("expected 1 resource removed, got %d", len(v.removeResourceIDs))
	}
}

func TestProcessResource_GetPledgesError(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)

	store := &mockReaperStore{
		pledgesWithUsersErr: map[string]error{
			"res1": fmt.Errorf("db error"),
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	// Should not remove resource on pledge fetch error
	if len(v.removeResourceIDs) != 0 {
		t.Errorf("expected no resources removed on pledge error, got %d", len(v.removeResourceIDs))
	}
}

func TestProcessResource_RemoveResourceError(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)

	store := &mockReaperStore{
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {},
		},
	}
	v := &mockReaperVault{removeResourceErr: fmt.Errorf("api error")}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	// Should attempt to remove resource (it fails but is called)
	if len(v.removeResourceIDs) != 1 {
		t.Errorf("expected 1 remove resource call, got %d", len(v.removeResourceIDs))
	}
}

func TestProcessResource_MultiplePledges(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	user1ID := uuid.NewV4()
	user2ID := uuid.NewV4()
	pledge1ID := uuid.NewV4()
	pledge2ID := uuid.NewV4()

	store := &mockReaperStore{
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {
				makePledge(pledge1ID, "res1", user1ID, 0.5, &models.User{
					UserID: user1ID,
					Email:  "user1@example.com",
				}),
				makePledge(pledge2ID, "res1", user2ID, 0.5, &models.User{
					UserID: user2ID,
					Email:  "user2@example.com",
				}),
			},
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	if len(v.removePledgeCalls) != 2 {
		t.Fatalf("expected 2 pledges removed, got %d", len(v.removePledgeCalls))
	}
	if len(n.calls) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(n.calls))
	}

	emails := map[string]bool{}
	for _, call := range n.calls {
		emails[call.email] = true
	}
	if !emails["user1@example.com"] || !emails["user2@example.com"] {
		t.Errorf("expected notifications for both users, got %v", n.calls)
	}
}

// --- Tests for removePledgeAndNotify ---

func TestRemovePledgeAndNotify_Success(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	userID := uuid.NewV4()
	pledgeID := uuid.NewV4()
	pledge := makePledge(pledgeID, "res1", userID, 1.0, &models.User{
		UserID: userID,
		Email:  "user@example.com",
	})

	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(nil, v, n)

	r.removePledgeAndNotify(context.Background(), pledge, resource, false)

	if len(v.removePledgeCalls) != 1 {
		t.Fatalf("expected 1 pledge removed, got %d", len(v.removePledgeCalls))
	}
	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].action != "expired" {
		t.Errorf("expected 'expired' action, got %q", n.calls[0].action)
	}
}

func TestRemovePledgeAndNotify_TransferTimeout(t *testing.T) {
	resource := makeResource("res1", nil)
	userID := uuid.NewV4()
	pledge := makePledge(uuid.NewV4(), "res1", userID, 1.0, &models.User{
		UserID: userID,
		Email:  "user@example.com",
	})

	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(nil, v, n)

	r.removePledgeAndNotify(context.Background(), pledge, resource, true)

	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].action != "transfer_timeout" {
		t.Errorf("expected 'transfer_timeout' action, got %q", n.calls[0].action)
	}
}

func TestRemovePledgeAndNotify_RemovePledgeError(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	userID := uuid.NewV4()
	pledge := makePledge(uuid.NewV4(), "res1", userID, 1.0, &models.User{
		UserID: userID,
		Email:  "user@example.com",
	})

	v := &mockReaperVault{removePledgeErr: fmt.Errorf("remove error")}
	n := &mockReaperNotification{}
	r := newTestReaper(nil, v, n)

	r.removePledgeAndNotify(context.Background(), pledge, resource, false)

	// Should not send notification if pledge removal fails
	if len(n.calls) != 0 {
		t.Errorf("expected no notifications on pledge removal error, got %d", len(n.calls))
	}
}

func TestRemovePledgeAndNotify_NilUser(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	pledge := makePledge(uuid.NewV4(), "res1", uuid.NewV4(), 1.0, nil)

	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(nil, v, n)

	r.removePledgeAndNotify(context.Background(), pledge, resource, false)

	// Pledge should be removed
	if len(v.removePledgeCalls) != 1 {
		t.Fatalf("expected 1 pledge removed, got %d", len(v.removePledgeCalls))
	}

	// No notification for nil user
	if len(n.calls) != 0 {
		t.Errorf("expected no notifications for nil user, got %d", len(n.calls))
	}
}

func TestRemovePledgeAndNotify_EmptyEmail(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	userID := uuid.NewV4()
	pledge := makePledge(uuid.NewV4(), "res1", userID, 1.0, &models.User{
		UserID: userID,
		Email:  "",
	})

	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(nil, v, n)

	r.removePledgeAndNotify(context.Background(), pledge, resource, false)

	// Pledge should be removed
	if len(v.removePledgeCalls) != 1 {
		t.Fatalf("expected 1 pledge removed, got %d", len(v.removePledgeCalls))
	}

	// No notification for empty email
	if len(n.calls) != 0 {
		t.Errorf("expected no notifications for empty email, got %d", len(n.calls))
	}
}

// --- Tests for sendNotification ---

func TestSendNotification_Expired(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)

	n := &mockReaperNotification{}
	r := newTestReaper(nil, nil, n)

	r.sendNotification("user@example.com", resource, false)

	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].action != "expired" {
		t.Errorf("expected 'expired' action, got %q", n.calls[0].action)
	}
	if n.calls[0].email != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %q", n.calls[0].email)
	}
	if n.calls[0].resourceID != "res1" {
		t.Errorf("expected resource ID 'res1', got %q", n.calls[0].resourceID)
	}
}

func TestSendNotification_TransferTimeout(t *testing.T) {
	resource := makeResource("res1", nil)

	n := &mockReaperNotification{}
	r := newTestReaper(nil, nil, n)

	r.sendNotification("user@example.com", resource, true)

	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].action != "transfer_timeout" {
		t.Errorf("expected 'transfer_timeout' action, got %q", n.calls[0].action)
	}
}

func TestSendNotification_ExpiredError(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)

	n := &mockReaperNotification{sendExpiredErr: fmt.Errorf("mail error")}
	r := newTestReaper(nil, nil, n)

	// Should not panic, just log the error
	r.sendNotification("user@example.com", resource, false)

	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification attempt, got %d", len(n.calls))
	}
}

func TestSendNotification_TransferTimeoutError(t *testing.T) {
	resource := makeResource("res1", nil)

	n := &mockReaperNotification{sendTransferTimeoutErr: fmt.Errorf("mail error")}
	r := newTestReaper(nil, nil, n)

	// Should not panic, just log the error
	r.sendNotification("user@example.com", resource, true)

	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification attempt, got %d", len(n.calls))
	}
}

// --- Tests for full reap flow ---

func TestRun_FullFlow_MixedResources(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	user1ID := uuid.NewV4()
	user2ID := uuid.NewV4()
	pledge1ID := uuid.NewV4()
	pledge2ID := uuid.NewV4()

	store := &mockReaperStore{
		expiredResources: []vaultModels.Resource{
			makeResource("expired-res", expired),    // has ExpiredAt -> expiration
			makeResource("timeout-res", nil),         // no ExpiredAt -> transfer timeout
		},
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"expired-res": {
				makePledge(pledge1ID, "expired-res", user1ID, 1.0, &models.User{
					UserID: user1ID,
					Email:  "user1@example.com",
				}),
			},
			"timeout-res": {
				makePledge(pledge2ID, "timeout-res", user2ID, 2.0, &models.User{
					UserID: user2ID,
					Email:  "user2@example.com",
				}),
			},
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.run(context.Background())

	// Both pledges should be removed
	if len(v.removePledgeCalls) != 2 {
		t.Fatalf("expected 2 pledges removed, got %d", len(v.removePledgeCalls))
	}

	// Both resources should be removed
	if len(v.removeResourceIDs) != 2 {
		t.Fatalf("expected 2 resources removed, got %d", len(v.removeResourceIDs))
	}

	// Should have 2 notifications with correct types
	if len(n.calls) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(n.calls))
	}

	notificationTypes := map[string]string{}
	for _, call := range n.calls {
		notificationTypes[call.email] = call.action
	}

	if notificationTypes["user1@example.com"] != "expired" {
		t.Errorf("expected 'expired' for user1, got %q", notificationTypes["user1@example.com"])
	}
	if notificationTypes["user2@example.com"] != "transfer_timeout" {
		t.Errorf("expected 'transfer_timeout' for user2, got %q", notificationTypes["user2@example.com"])
	}
}

func TestRun_PartialPledgeRemovalFailure(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	user1ID := uuid.NewV4()
	user2ID := uuid.NewV4()
	pledge1ID := uuid.NewV4()
	pledge2ID := uuid.NewV4()

	store := &mockReaperStore{
		expiredResources: []vaultModels.Resource{
			makeResource("res1", expired),
		},
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {
				makePledge(pledge1ID, "res1", user1ID, 0.5, &models.User{
					UserID: user1ID,
					Email:  "user1@example.com",
				}),
				makePledge(pledge2ID, "res1", user2ID, 0.5, &models.User{
					UserID: user2ID,
					Email:  "user2@example.com",
				}),
			},
		},
	}
	v := &mockReaperVault{
		removePledgeErrMap: map[uuid.UUID]error{
			pledge1ID: fmt.Errorf("remove error for pledge1"),
		},
	}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.run(context.Background())

	// Both pledge removals attempted
	if len(v.removePledgeCalls) != 2 {
		t.Fatalf("expected 2 pledge removal attempts, got %d", len(v.removePledgeCalls))
	}

	// Only one notification (pledge2 succeeded, pledge1 failed)
	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification (only successful pledge), got %d", len(n.calls))
	}
	if n.calls[0].email != "user2@example.com" {
		t.Errorf("expected notification for user2, got %q", n.calls[0].email)
	}

	// Resource should still be removed
	if len(v.removeResourceIDs) != 1 {
		t.Errorf("expected resource to still be removed, got %d removals", len(v.removeResourceIDs))
	}
}

func TestProcessResource_PledgeWithUserNoEmail_And_PledgeWithEmail(t *testing.T) {
	expired := timePtr(time.Now().Add(-8 * 24 * time.Hour))
	resource := makeResource("res1", expired)
	user1ID := uuid.NewV4()
	user2ID := uuid.NewV4()

	store := &mockReaperStore{
		pledgesWithUsers: map[string][]vaultModels.Pledge{
			"res1": {
				makePledge(uuid.NewV4(), "res1", user1ID, 0.5, &models.User{
					UserID: user1ID,
					Email:  "", // empty email
				}),
				makePledge(uuid.NewV4(), "res1", user2ID, 0.5, &models.User{
					UserID: user2ID,
					Email:  "user2@example.com",
				}),
			},
		},
	}
	v := &mockReaperVault{}
	n := &mockReaperNotification{}
	r := newTestReaper(store, v, n)

	r.processResource(context.Background(), resource)

	// Both pledges should be removed
	if len(v.removePledgeCalls) != 2 {
		t.Fatalf("expected 2 pledges removed, got %d", len(v.removePledgeCalls))
	}

	// Only 1 notification (user2 has email)
	if len(n.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(n.calls))
	}
	if n.calls[0].email != "user2@example.com" {
		t.Errorf("expected notification for user2, got %q", n.calls[0].email)
	}
}