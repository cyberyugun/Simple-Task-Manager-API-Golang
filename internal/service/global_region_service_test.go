package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newRegionTestService(t *testing.T) (*GlobalRegionService,int64,int64) {
	t.Helper()
	orgs:=repository.NewInMemoryOrganizationRepository()
	gov:=repository.NewInMemoryGovernanceRepository()
	regions:=repository.NewInMemoryGlobalRegionRepository()
	now:=time.Now().UTC()
	org,err:=orgs.CreateOrganization(model.Organization{Name:"test",Status:model.OrganizationStatusActive,OwnerUserID:1,MaxWorkspaces:10,MaxMembers:10,CreatedByUserID:1,CreatedAt:now,UpdatedAt:now})
	if err!=nil{t.Fatal(err)}
	return NewGlobalRegionService(regions,orgs,gov),org.ID,1
}

func TestGlobalRegionPolicyAndRoute(t *testing.T){
	s,orgID,userID:=newRegionTestService(t)
	policy,err:=s.UpdatePolicy(userID,orgID,model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion:"ap-southeast",AllowedRegions:[]string{"ap-southeast","ap-northeast"},FailoverRegions:[]string{"ap-northeast"},
		DataResidencyEnforced:true,CrossRegionApprovalRequired:true,RPOSeconds:300,RTOSeconds:1800,
	})
	if err!=nil{t.Fatal(err)}
	if policy.HomeRegion!="ap-southeast"{t.Fatalf("unexpected home region %q",policy.HomeRegion)}
	route,err:=s.Route(userID,orgID);if err!=nil{t.Fatal(err)}
	if route.PrimaryRegion!="ap-southeast"||route.FailoverRegion!="ap-northeast"{t.Fatalf("unexpected route %#v",route)}
}

func TestGlobalRegionMigrationRequiresApproval(t *testing.T){
	s,orgID,userID:=newRegionTestService(t)
	_,err:=s.UpdatePolicy(userID,orgID,model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion:"ap-southeast",AllowedRegions:[]string{"ap-southeast","ap-northeast"},FailoverRegions:[]string{"ap-northeast"},
		DataResidencyEnforced:true,CrossRegionApprovalRequired:true,RPOSeconds:300,RTOSeconds:1800,
	});if err!=nil{t.Fatal(err)}
	migration,err:=s.CreateMigration(userID,orgID,model.CreateRegionMigrationRequest{
		Scope:"organization",SourceRegion:"ap-southeast",TargetRegion:"ap-northeast",Reason:"residency move",
	});if err!=nil{t.Fatal(err)}
	if migration.Status!=model.RegionMigrationPendingApproval{t.Fatalf("unexpected status %q",migration.Status)}
	migration,err=s.DecideMigration(userID,orgID,migration.ID,model.DecideRegionMigrationRequest{Decision:"approve"});if err!=nil{t.Fatal(err)}
	if migration.Status!=model.RegionMigrationApproved{t.Fatalf("unexpected approved status %q",migration.Status)}
	migration,err=s.CompleteMigration(userID,orgID,migration.ID,model.CompleteRegionMigrationRequest{Success:true,Checkpoint:map[string]any{"verified":true}});if err!=nil{t.Fatal(err)}
	if migration.Status!=model.RegionMigrationCompleted{t.Fatalf("unexpected completed status %q",migration.Status)}
	policy,err:=s.Policy(userID,orgID);if err!=nil{t.Fatal(err)}
	if policy.HomeRegion!="ap-northeast"{t.Fatalf("home region not updated: %q",policy.HomeRegion)}
}

func TestGlobalRegionRejectsUnknownAllowedTarget(t *testing.T){
	s,orgID,userID:=newRegionTestService(t)
	_,err:=s.UpdatePolicy(userID,orgID,model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion:"ap-southeast",AllowedRegions:[]string{"ap-southeast"},DataResidencyEnforced:true,
		CrossRegionApprovalRequired:true,RPOSeconds:300,RTOSeconds:1800,
	});if err!=nil{t.Fatal(err)}
	_,err=s.CreateTransfer(userID,orgID,model.CreateCrossRegionTransferRequest{
		ResourceType:"task",ResourceID:"1",Classification:"internal",SourceRegion:"ap-southeast",TargetRegion:"eu-west",Reason:"test",
	})
	if !errors.Is(err,ErrInvalidCrossRegionTransfer){t.Fatalf("expected invalid transfer, got %v",err)}
}
