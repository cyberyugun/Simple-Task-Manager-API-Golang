package service

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrRegionForbidden = errors.New("region administration requires organization owner or admin")
	ErrInvalidRegionPolicy = errors.New("invalid organization region policy")
	ErrRegionResidencyViolation = errors.New("region violates workspace data-residency policy")
	ErrInvalidRegionalPlacement = errors.New("invalid regional placement")
	ErrInvalidRegionMigration = errors.New("invalid region migration")
	ErrInvalidCrossRegionTransfer = errors.New("invalid cross-region transfer")
	ErrInvalidFailoverExercise = errors.New("invalid failover exercise")
	ErrRegionDecisionConflict = errors.New("region workflow is not in a decision-compatible state")
)

var regionKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

type GlobalRegionService struct {
	repo repository.GlobalRegionRepository
	orgs repository.OrganizationRepository
	governance repository.GovernanceRepository
}

func NewGlobalRegionService(repo repository.GlobalRegionRepository, orgs repository.OrganizationRepository, governance repository.GovernanceRepository) *GlobalRegionService {
	return &GlobalRegionService{repo:repo,orgs:orgs,governance:governance}
}

func (s *GlobalRegionService) Regions() []model.GlobalRegion {
	return []model.GlobalRegion{
		{Key:"us-east",Provider:"provider-neutral",Geography:"North America",DisplayName:"US East"},
		{Key:"us-west",Provider:"provider-neutral",Geography:"North America",DisplayName:"US West"},
		{Key:"eu-west",Provider:"provider-neutral",Geography:"Europe",DisplayName:"EU West"},
		{Key:"eu-central",Provider:"provider-neutral",Geography:"Europe",DisplayName:"EU Central"},
		{Key:"ap-southeast",Provider:"provider-neutral",Geography:"Asia Pacific",DisplayName:"AP Southeast"},
		{Key:"ap-northeast",Provider:"provider-neutral",Geography:"Asia Pacific",DisplayName:"AP Northeast"},
		{Key:"au-east",Provider:"provider-neutral",Geography:"Australia",DisplayName:"Australia East"},
	}
}

func defaultRegionPolicy(organizationID int64) model.OrganizationRegionPolicy {
	return model.OrganizationRegionPolicy{
		OrganizationID:organizationID,AllowedRegions:[]string{},FailoverRegions:[]string{},
		DataResidencyEnforced:true,CrossRegionApprovalRequired:true,RPOSeconds:300,RTOSeconds:1800,
	}
}

func (s *GlobalRegionService) Policy(actorUserID,organizationID int64)(model.OrganizationRegionPolicy,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.OrganizationRegionPolicy{},err}
	item,err:=s.repo.GetPolicy(organizationID)
	if errors.Is(err,repository.ErrRegionPolicyNotFound){return defaultRegionPolicy(organizationID),nil}
	return item,err
}

func (s *GlobalRegionService) UpdatePolicy(actorUserID,organizationID int64,req model.UpdateOrganizationRegionPolicyRequest)(model.OrganizationRegionPolicy,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.OrganizationRegionPolicy{},err}
	home:=normalizeRegion(req.HomeRegion)
	allowed:=normalizeRegions(req.AllowedRegions)
	failover:=normalizeRegions(req.FailoverRegions)
	if home==""||len(allowed)==0||!containsString(allowed,home)||req.RPOSeconds<1||req.RPOSeconds>86400||
		req.RTOSeconds<1||req.RTOSeconds>604800||containsString(failover,home){
		return model.OrganizationRegionPolicy{},ErrInvalidRegionPolicy
	}
	for _,r:=range allowed{if !validRegionKey(r){return model.OrganizationRegionPolicy{},ErrInvalidRegionPolicy}}
	for _,r:=range failover{if !containsString(allowed,r){return model.OrganizationRegionPolicy{},ErrInvalidRegionPolicy}}
	if req.DataResidencyEnforced {
		for _,r:=range allowed{if err:=s.validateRegionAgainstGovernance(organizationID,r);err!=nil{return model.OrganizationRegionPolicy{},err}}
	}
	now:=time.Now().UTC()
	old,err:=s.repo.GetPolicy(organizationID);createdAt:=now
	if err==nil{createdAt=old.CreatedAt}else if !errors.Is(err,repository.ErrRegionPolicyNotFound){return model.OrganizationRegionPolicy{},err}
	item,err:=s.repo.UpsertPolicy(model.OrganizationRegionPolicy{
		OrganizationID:organizationID,HomeRegion:home,AllowedRegions:allowed,FailoverRegions:failover,
		DataResidencyEnforced:req.DataResidencyEnforced,CrossRegionApprovalRequired:req.CrossRegionApprovalRequired,
		RPOSeconds:req.RPOSeconds,RTOSeconds:req.RTOSeconds,UpdatedByUserID:actorUserID,CreatedAt:createdAt,UpdatedAt:now,
	})
	if err==nil{s.audit(organizationID,actorUserID,"region.policy.updated","region_policy",fmt.Sprint(organizationID),map[string]any{
		"home_region":home,"allowed_regions":allowed,"failover_regions":failover,"rpo_seconds":req.RPOSeconds,"rto_seconds":req.RTOSeconds,
	})}
	return item,err
}

func (s *GlobalRegionService) Placements(actorUserID,organizationID int64)([]model.RegionalPlacement,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return nil,err}
	return s.repo.ListPlacements(organizationID)
}

func (s *GlobalRegionService) UpsertPlacement(actorUserID,organizationID int64,req model.UpsertRegionalPlacementRequest)(model.RegionalPlacement,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.RegionalPlacement{},err}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.RegionalPlacement{},err}
	resourceType:=strings.ToLower(strings.TrimSpace(req.ResourceType));resourceID:=strings.TrimSpace(req.ResourceID)
	primary:=normalizeRegion(req.PrimaryRegion);replicas:=normalizeRegions(req.ReplicaRegions)
	status:=strings.ToLower(strings.TrimSpace(req.Status));if status==""{status=model.RegionalPlacementActive}
	if resourceType==""||resourceID==""||len(resourceType)>80||len(resourceID)>200||!containsString(policy.AllowedRegions,primary)||
		(status!=model.RegionalPlacementActive&&status!=model.RegionalPlacementMigrating&&status!=model.RegionalPlacementDegraded){
		return model.RegionalPlacement{},ErrInvalidRegionalPlacement
	}
	for _,r:=range replicas{if r==primary||!containsString(policy.AllowedRegions,r){return model.RegionalPlacement{},ErrInvalidRegionalPlacement}}
	if policy.DataResidencyEnforced{
		if err:=s.validateRegionAgainstGovernance(organizationID,primary);err!=nil{return model.RegionalPlacement{},err}
		for _,r:=range replicas{if err:=s.validateRegionAgainstGovernance(organizationID,r);err!=nil{return model.RegionalPlacement{},err}}
	}
	now:=time.Now().UTC()
	item,err:=s.repo.UpsertPlacement(model.RegionalPlacement{
		OrganizationID:organizationID,ResourceType:resourceType,ResourceID:resourceID,PrimaryRegion:primary,
		ReplicaRegions:replicas,Status:status,UpdatedByUserID:actorUserID,CreatedAt:now,UpdatedAt:now,LastVerifiedAt:&now,
	})
	if err==nil{s.audit(organizationID,actorUserID,"region.placement.upserted","regional_placement",resourceType+":"+resourceID,map[string]any{"primary_region":primary,"replica_regions":replicas})}
	return item,err
}

func (s *GlobalRegionService) Migrations(actorUserID,organizationID int64)([]model.RegionMigration,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return nil,err}
	return s.repo.ListMigrations(organizationID,200)
}

func (s *GlobalRegionService) CreateMigration(actorUserID,organizationID int64,req model.CreateRegionMigrationRequest)(model.RegionMigration,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.RegionMigration{},err}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.RegionMigration{},err}
	scope:=strings.ToLower(strings.TrimSpace(req.Scope));source:=normalizeRegion(req.SourceRegion);target:=normalizeRegion(req.TargetRegion)
	resourceType:=strings.ToLower(strings.TrimSpace(req.ResourceType));resourceID:=strings.TrimSpace(req.ResourceID);reason:=strings.TrimSpace(req.Reason)
	if (scope!="organization"&&scope!="resource")||source==""||target==""||source==target||reason==""||len(reason)>4000||
		!containsString(policy.AllowedRegions,target)||(scope=="resource"&&(resourceType==""||resourceID=="")){
		return model.RegionMigration{},ErrInvalidRegionMigration
	}
	if policy.DataResidencyEnforced{if err:=s.validateRegionAgainstGovernance(organizationID,target);err!=nil{return model.RegionMigration{},err}}
	status:=model.RegionMigrationApproved
	if policy.CrossRegionApprovalRequired{status=model.RegionMigrationPendingApproval}
	now:=time.Now().UTC()
	item,err:=s.repo.CreateMigration(model.RegionMigration{
		OrganizationID:organizationID,Scope:scope,ResourceType:resourceType,ResourceID:resourceID,SourceRegion:source,TargetRegion:target,
		Status:status,Reason:reason,Checkpoint:map[string]any{},RequestedByUserID:actorUserID,RequestedAt:now,UpdatedAt:now,
	})
	if err==nil{s.audit(organizationID,actorUserID,"region.migration.requested","region_migration",fmt.Sprint(item.ID),map[string]any{"scope":scope,"source_region":source,"target_region":target,"status":status})}
	return item,err
}

func (s *GlobalRegionService) DecideMigration(actorUserID,organizationID,migrationID int64,req model.DecideRegionMigrationRequest)(model.RegionMigration,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.RegionMigration{},err}
	item,err:=s.repo.GetMigration(organizationID,migrationID);if err!=nil{return model.RegionMigration{},err}
	if item.Status!=model.RegionMigrationPendingApproval{return model.RegionMigration{},ErrRegionDecisionConflict}
	decision:=strings.ToLower(strings.TrimSpace(req.Decision));if decision!="approve"&&decision!="reject"{return model.RegionMigration{},ErrInvalidRegionMigration}
	now:=time.Now().UTC();item.DecidedByUserID=&actorUserID;item.DecisionComment=strings.TrimSpace(req.Comment);item.DecidedAt=&now;item.UpdatedAt=now
	if decision=="approve"{item.Status=model.RegionMigrationApproved}else{item.Status=model.RegionMigrationRejected}
	item,err=s.repo.UpdateMigration(item)
	if err==nil{s.audit(organizationID,actorUserID,"region.migration."+item.Status,"region_migration",fmt.Sprint(item.ID),nil)}
	return item,err
}

func (s *GlobalRegionService) CompleteMigration(actorUserID,organizationID,migrationID int64,req model.CompleteRegionMigrationRequest)(model.RegionMigration,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.RegionMigration{},err}
	item,err:=s.repo.GetMigration(organizationID,migrationID);if err!=nil{return model.RegionMigration{},err}
	if item.Status!=model.RegionMigrationApproved&&item.Status!=model.RegionMigrationRunning{return model.RegionMigration{},ErrRegionDecisionConflict}
	now:=time.Now().UTC();item.Checkpoint=req.Checkpoint;item.CompletedAt=&now;item.UpdatedAt=now
	if req.Success{item.Status=model.RegionMigrationCompleted}else{item.Status=model.RegionMigrationFailed}
	item,err=s.repo.UpdateMigration(item);if err!=nil{return model.RegionMigration{},err}
	if req.Success&&item.Scope=="organization"{
		policy,perr:=s.repo.GetPolicy(organizationID)
		if perr!=nil{return model.RegionMigration{},perr}
		policy.HomeRegion=item.TargetRegion;policy.UpdatedByUserID=actorUserID;policy.UpdatedAt=now
		if _,perr=s.repo.UpsertPolicy(policy);perr!=nil{return model.RegionMigration{},perr}
	}
	s.audit(organizationID,actorUserID,"region.migration."+item.Status,"region_migration",fmt.Sprint(item.ID),map[string]any{"target_region":item.TargetRegion})
	return item,nil
}

func (s *GlobalRegionService) Transfers(actorUserID,organizationID int64)([]model.CrossRegionTransfer,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return nil,err}
	return s.repo.ListTransfers(organizationID,200)
}

func (s *GlobalRegionService) CreateTransfer(actorUserID,organizationID int64,req model.CreateCrossRegionTransferRequest)(model.CrossRegionTransfer,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.CrossRegionTransfer{},err}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.CrossRegionTransfer{},err}
	resourceType:=strings.ToLower(strings.TrimSpace(req.ResourceType));resourceID:=strings.TrimSpace(req.ResourceID)
	classification:=strings.ToLower(strings.TrimSpace(req.Classification));source:=normalizeRegion(req.SourceRegion);target:=normalizeRegion(req.TargetRegion);reason:=strings.TrimSpace(req.Reason)
	if resourceType==""||resourceID==""||classification==""||source==""||target==""||source==target||reason==""||
		!containsString(policy.AllowedRegions,target){return model.CrossRegionTransfer{},ErrInvalidCrossRegionTransfer}
	if policy.DataResidencyEnforced{if err:=s.validateRegionAgainstGovernance(organizationID,target);err!=nil{return model.CrossRegionTransfer{},err}}
	needsApproval:=policy.CrossRegionApprovalRequired||s.workspaceRequiresTransferApproval(organizationID)
	status:=model.RegionTransferApproved;if needsApproval{status=model.RegionTransferPendingApproval}
	now:=time.Now().UTC()
	item,err:=s.repo.CreateTransfer(model.CrossRegionTransfer{
		OrganizationID:organizationID,ResourceType:resourceType,ResourceID:resourceID,Classification:classification,
		SourceRegion:source,TargetRegion:target,Status:status,Reason:reason,RequestedByUserID:actorUserID,RequestedAt:now,UpdatedAt:now,
	})
	if err==nil{s.audit(organizationID,actorUserID,"region.transfer.requested","cross_region_transfer",fmt.Sprint(item.ID),map[string]any{"source_region":source,"target_region":target,"classification":classification,"status":status})}
	return item,err
}

func (s *GlobalRegionService) DecideTransfer(actorUserID,organizationID,transferID int64,req model.DecideCrossRegionTransferRequest)(model.CrossRegionTransfer,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.CrossRegionTransfer{},err}
	item,err:=s.repo.GetTransfer(organizationID,transferID);if err!=nil{return model.CrossRegionTransfer{},err}
	if item.Status!=model.RegionTransferPendingApproval{return model.CrossRegionTransfer{},ErrRegionDecisionConflict}
	decision:=strings.ToLower(strings.TrimSpace(req.Decision));if decision!="approve"&&decision!="reject"{return model.CrossRegionTransfer{},ErrInvalidCrossRegionTransfer}
	now:=time.Now().UTC();item.DecidedByUserID=&actorUserID;item.DecisionComment=strings.TrimSpace(req.Comment);item.DecidedAt=&now;item.UpdatedAt=now
	if decision=="approve"{item.Status=model.RegionTransferApproved}else{item.Status=model.RegionTransferRejected}
	item,err=s.repo.UpdateTransfer(item);if err==nil{s.audit(organizationID,actorUserID,"region.transfer."+item.Status,"cross_region_transfer",fmt.Sprint(item.ID),nil)}
	return item,err
}

func (s *GlobalRegionService) CompleteTransfer(actorUserID,organizationID,transferID int64)(model.CrossRegionTransfer,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.CrossRegionTransfer{},err}
	item,err:=s.repo.GetTransfer(organizationID,transferID);if err!=nil{return model.CrossRegionTransfer{},err}
	if item.Status!=model.RegionTransferApproved{return model.CrossRegionTransfer{},ErrRegionDecisionConflict}
	now:=time.Now().UTC();item.Status=model.RegionTransferCompleted;item.CompletedAt=&now;item.UpdatedAt=now
	item,err=s.repo.UpdateTransfer(item);if err==nil{s.audit(organizationID,actorUserID,"region.transfer.completed","cross_region_transfer",fmt.Sprint(item.ID),nil)}
	return item,err
}

func (s *GlobalRegionService) FailoverExercises(actorUserID,organizationID int64)([]model.FailoverExercise,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return nil,err}
	return s.repo.ListFailoverExercises(organizationID,100)
}

func (s *GlobalRegionService) CreateFailoverExercise(actorUserID,organizationID int64,req model.CreateFailoverExerciseRequest)(model.FailoverExercise,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.FailoverExercise{},err}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.FailoverExercise{},err}
	source:=normalizeRegion(req.SourceRegion);target:=normalizeRegion(req.TargetRegion)
	if source==""||target==""||source==target||!containsString(policy.FailoverRegions,target)||len(strings.TrimSpace(req.Notes))>4000{
		return model.FailoverExercise{},ErrInvalidFailoverExercise
	}
	now:=time.Now().UTC()
	item,err:=s.repo.CreateFailoverExercise(model.FailoverExercise{
		OrganizationID:organizationID,SourceRegion:source,TargetRegion:target,Status:model.FailoverExercisePlanned,
		Notes:strings.TrimSpace(req.Notes),CreatedByUserID:actorUserID,CreatedAt:now,
	})
	if err==nil{s.audit(organizationID,actorUserID,"region.failover_exercise.created","failover_exercise",fmt.Sprint(item.ID),map[string]any{"source_region":source,"target_region":target})}
	return item,err
}

func (s *GlobalRegionService) CompleteFailoverExercise(actorUserID,organizationID,exerciseID int64,req model.CompleteFailoverExerciseRequest)(model.FailoverExercise,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.FailoverExercise{},err}
	if req.AchievedRPOSeconds<0||req.AchievedRTOSeconds<0{return model.FailoverExercise{},ErrInvalidFailoverExercise}
	item,err:=s.repo.GetFailoverExercise(organizationID,exerciseID);if err!=nil{return model.FailoverExercise{},err}
	if item.Status!=model.FailoverExercisePlanned{return model.FailoverExercise{},ErrRegionDecisionConflict}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.FailoverExercise{},err}
	now:=time.Now().UTC();item.AchievedRPOSeconds=req.AchievedRPOSeconds;item.AchievedRTOSeconds=req.AchievedRTOSeconds
	item.MeetsRPO=req.Success&&req.AchievedRPOSeconds<=policy.RPOSeconds;item.MeetsRTO=req.Success&&req.AchievedRTOSeconds<=policy.RTOSeconds;item.CompletedAt=&now
	if req.Success{item.Status=model.FailoverExerciseCompleted}else{item.Status=model.FailoverExerciseFailed}
	item,err=s.repo.UpdateFailoverExercise(item);if err==nil{s.audit(organizationID,actorUserID,"region.failover_exercise."+item.Status,"failover_exercise",fmt.Sprint(item.ID),map[string]any{"meets_rpo":item.MeetsRPO,"meets_rto":item.MeetsRTO})}
	return item,err
}

func (s *GlobalRegionService) Route(actorUserID,organizationID int64)(model.RegionRouteDecision,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.RegionRouteDecision{},err}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.RegionRouteDecision{},err}
	failover:="";if len(policy.FailoverRegions)>0{failover=policy.FailoverRegions[0]}
	return model.RegionRouteDecision{OrganizationID:organizationID,PrimaryRegion:policy.HomeRegion,FailoverRegion:failover,
		AllowedRegions:append([]string(nil),policy.AllowedRegions...),ResidencyEnforced:policy.DataResidencyEnforced,GeneratedAt:time.Now().UTC()},nil
}

func (s *GlobalRegionService) Report(actorUserID,organizationID int64)(model.RegionComplianceReport,error){
	if _,err:=s.requireAdmin(actorUserID,organizationID);err!=nil{return model.RegionComplianceReport{},err}
	policy,err:=s.requireConfiguredPolicy(organizationID);if err!=nil{return model.RegionComplianceReport{},err}
	placements,err:=s.repo.ListPlacements(organizationID);if err!=nil{return model.RegionComplianceReport{},err}
	violations:=make([]model.ResidencyViolation,0)
	if err:=s.validateRegionAgainstGovernance(organizationID,policy.HomeRegion);err!=nil{
		violations=append(violations,model.ResidencyViolation{Region:policy.HomeRegion,Message:"organization home region is not allowed by at least one attached workspace"})
	}
	for _,p:=range placements{
		if !containsString(policy.AllowedRegions,p.PrimaryRegion){
			violations=append(violations,model.ResidencyViolation{ResourceType:p.ResourceType,ResourceID:p.ResourceID,Region:p.PrimaryRegion,Message:"primary region is outside organization allowed regions"})
			continue
		}
		if err:=s.validateRegionAgainstGovernance(organizationID,p.PrimaryRegion);err!=nil{
			violations=append(violations,model.ResidencyViolation{ResourceType:p.ResourceType,ResourceID:p.ResourceID,Region:p.PrimaryRegion,Message:"placement conflicts with workspace residency policy"})
		}
		for _,r:=range p.ReplicaRegions{
			if !containsString(policy.AllowedRegions,r){violations=append(violations,model.ResidencyViolation{ResourceType:p.ResourceType,ResourceID:p.ResourceID,Region:r,Message:"replica region is outside organization allowed regions"})}
		}
	}
	return model.RegionComplianceReport{OrganizationID:organizationID,Policy:policy,Placements:placements,Violations:violations,Compliant:len(violations)==0,GeneratedAt:time.Now().UTC()},nil
}

func (s *GlobalRegionService) requireConfiguredPolicy(organizationID int64)(model.OrganizationRegionPolicy,error){
	item,err:=s.repo.GetPolicy(organizationID)
	if errors.Is(err,repository.ErrRegionPolicyNotFound){return model.OrganizationRegionPolicy{},ErrInvalidRegionPolicy}
	if err!=nil{return model.OrganizationRegionPolicy{},err}
	if item.HomeRegion==""||len(item.AllowedRegions)==0{return model.OrganizationRegionPolicy{},ErrInvalidRegionPolicy}
	return item,nil
}

func (s *GlobalRegionService) validateRegionAgainstGovernance(organizationID int64,region string) error {
	links,err:=s.orgs.ListWorkspaces(organizationID);if err!=nil{return err}
	for _,link:=range links{
		policy,err:=s.governance.GetPolicy(link.WorkspaceID)
		if errors.Is(err,repository.ErrGovernancePolicyNotFound){continue}
		if err!=nil{return err}
		if len(policy.AllowedDataRegions)>0&&!containsString(normalizeRegions(policy.AllowedDataRegions),region){return ErrRegionResidencyViolation}
	}
	return nil
}

func (s *GlobalRegionService) workspaceRequiresTransferApproval(organizationID int64) bool {
	links,err:=s.orgs.ListWorkspaces(organizationID);if err!=nil{return true}
	for _,link:=range links{
		policy,err:=s.governance.GetPolicy(link.WorkspaceID)
		if err==nil&&policy.RestrictCrossRegionTransfer{return true}
	}
	return false
}

func (s *GlobalRegionService) requireAdmin(userID,organizationID int64)(model.Organization,error){
	org,err:=s.orgs.GetOrganization(organizationID);if err!=nil{return model.Organization{},err}
	member,err:=s.orgs.GetMember(organizationID,userID);if err!=nil{return model.Organization{},ErrRegionForbidden}
	if org.Status!=model.OrganizationStatusActive{return model.Organization{},ErrRegionForbidden}
	if member.Role!=model.OrganizationRoleOwner&&member.Role!=model.OrganizationRoleAdmin&&member.Role!=model.OrganizationRoleDelegatedAdmin{return model.Organization{},ErrRegionForbidden}
	return org,nil
}
func (s *GlobalRegionService) audit(organizationID,actorUserID int64,action,resourceType,resourceID string,metadata map[string]any){
	uid:=actorUserID;_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{OrganizationID:organizationID,ActorUserID:&uid,Action:action,ResourceType:resourceType,ResourceID:resourceID,Metadata:metadata,CreatedAt:time.Now().UTC()})
}
func normalizeRegion(v string) string { v=strings.ToLower(strings.TrimSpace(v)); if !validRegionKey(v){return ""}; return v }
func validRegionKey(v string) bool { return regionKeyPattern.MatchString(v) }
func normalizeRegions(values []string) []string {
	seen:=map[string]bool{};out:=make([]string,0,len(values))
	for _,v:=range values{v=normalizeRegion(v);if v==""||seen[v]{continue};seen[v]=true;out=append(out,v)}
	sort.Strings(out);return out
}
func containsString(values []string,target string) bool { for _,v:=range values{if v==target{return true}};return false }
