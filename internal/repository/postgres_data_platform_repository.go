package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresDataPlatformRepository struct {
	db *sql.DB
}

func NewPostgresDataPlatformRepository(db *sql.DB) *PostgresDataPlatformRepository {
	return &PostgresDataPlatformRepository{db: db}
}

func (r *PostgresDataPlatformRepository) CreateConnection(item model.DataPlatformConnection) (model.DataPlatformConnection, error) {
	bi, _ := json.Marshal(item.BIContracts)
	cfg, _ := json.Marshal(item.Config)
	mask, _ := json.Marshal(item.Masking)
	var biOut, cfgOut, maskOut []byte
	err := r.db.QueryRow(`
		INSERT INTO data_platform_connections (
			organization_id,name,provider,target,bi_contracts,config,secret_ref,status,masking,
			freshness_slo_minutes,max_monthly_cost_usd,created_by_user_id,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9::jsonb,$10,$11,$12,$13,$14)
		RETURNING id,organization_id,name,provider,target,bi_contracts,config,secret_ref,status,masking,
		          freshness_slo_minutes,max_monthly_cost_usd,created_by_user_id,created_at,updated_at
	`, item.OrganizationID, item.Name, item.Provider, item.Target, string(bi), string(cfg), item.SecretRef, item.Status,
		string(mask), item.FreshnessSLOMinutes, item.MaxMonthlyCostUSD, item.CreatedByUserID, item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID,&item.OrganizationID,&item.Name,&item.Provider,&item.Target,&biOut,&cfgOut,&item.SecretRef,&item.Status,
		&maskOut,&item.FreshnessSLOMinutes,&item.MaxMonthlyCostUSD,&item.CreatedByUserID,&item.CreatedAt,&item.UpdatedAt)
	if err != nil { return model.DataPlatformConnection{}, err }
	if err=json.Unmarshal(biOut,&item.BIContracts); err!=nil{return model.DataPlatformConnection{},err}
	if err=json.Unmarshal(cfgOut,&item.Config); err!=nil{return model.DataPlatformConnection{},err}
	if err=json.Unmarshal(maskOut,&item.Masking); err!=nil{return model.DataPlatformConnection{},err}
	return item,nil
}

func (r *PostgresDataPlatformRepository) GetConnection(organizationID, connectionID int64) (model.DataPlatformConnection,error) {
	item,err:=scanDataPlatformConnection(r.db.QueryRow(`
		SELECT id,organization_id,name,provider,target,bi_contracts,config,secret_ref,status,masking,
		       freshness_slo_minutes,max_monthly_cost_usd,created_by_user_id,created_at,updated_at
		FROM data_platform_connections WHERE organization_id=$1 AND id=$2
	`,organizationID,connectionID))
	if errors.Is(err,sql.ErrNoRows){return model.DataPlatformConnection{},ErrDataPlatformConnectionNotFound}
	return item,err
}

func (r *PostgresDataPlatformRepository) ListConnections(organizationID int64)([]model.DataPlatformConnection,error){
	rows,err:=r.db.Query(`
		SELECT id,organization_id,name,provider,target,bi_contracts,config,secret_ref,status,masking,
		       freshness_slo_minutes,max_monthly_cost_usd,created_by_user_id,created_at,updated_at
		FROM data_platform_connections WHERE organization_id=$1 ORDER BY id
	`,organizationID)
	if err!=nil{return nil,err}
	defer rows.Close()
	items:=[]model.DataPlatformConnection{}
	for rows.Next(){
		item,err:=scanDataPlatformConnection(rows); if err!=nil{return nil,err}
		items=append(items,item)
	}
	return items,rows.Err()
}

type dataPlatformScanner interface{ Scan(dest ...any) error }

func scanDataPlatformConnection(s dataPlatformScanner)(model.DataPlatformConnection,error){
	var item model.DataPlatformConnection
	var bi,cfg,mask []byte
	err:=s.Scan(&item.ID,&item.OrganizationID,&item.Name,&item.Provider,&item.Target,&bi,&cfg,&item.SecretRef,&item.Status,
		&mask,&item.FreshnessSLOMinutes,&item.MaxMonthlyCostUSD,&item.CreatedByUserID,&item.CreatedAt,&item.UpdatedAt)
	if err!=nil{return item,err}
	if err=json.Unmarshal(bi,&item.BIContracts);err!=nil{return item,err}
	if err=json.Unmarshal(cfg,&item.Config);err!=nil{return item,err}
	if err=json.Unmarshal(mask,&item.Masking);err!=nil{return item,err}
	return item,nil
}

func (r *PostgresDataPlatformRepository) CreateSchema(item model.DatasetSchemaVersion)(model.DatasetSchemaVersion,error){
	fields,_:=json.Marshal(item.Fields)
	var out []byte
	err:=r.db.QueryRow(`
		INSERT INTO data_platform_schema_versions (
			organization_id,connection_id,version,compatibility,status,fields,created_by_user_id,created_at,deprecated_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)
		RETURNING id,organization_id,connection_id,version,compatibility,status,fields,created_by_user_id,created_at,deprecated_at
	`,item.OrganizationID,item.ConnectionID,item.Version,item.Compatibility,item.Status,string(fields),item.CreatedByUserID,item.CreatedAt,item.DeprecatedAt).
		Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Version,&item.Compatibility,&item.Status,&out,&item.CreatedByUserID,&item.CreatedAt,&item.DeprecatedAt)
	if err!=nil{return model.DatasetSchemaVersion{},err}
	if err=json.Unmarshal(out,&item.Fields);err!=nil{return model.DatasetSchemaVersion{},err}
	return item,nil
}

func (r *PostgresDataPlatformRepository) ListSchemas(organizationID,connectionID int64)([]model.DatasetSchemaVersion,error){
	rows,err:=r.db.Query(`
		SELECT id,organization_id,connection_id,version,compatibility,status,fields,created_by_user_id,created_at,deprecated_at
		FROM data_platform_schema_versions WHERE organization_id=$1 AND connection_id=$2 ORDER BY version DESC
	`,organizationID,connectionID)
	if err!=nil{return nil,err}; defer rows.Close()
	items:=[]model.DatasetSchemaVersion{}
	for rows.Next(){
		var item model.DatasetSchemaVersion; var raw []byte
		if err:=rows.Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Version,&item.Compatibility,&item.Status,&raw,&item.CreatedByUserID,&item.CreatedAt,&item.DeprecatedAt);err!=nil{return nil,err}
		if err:=json.Unmarshal(raw,&item.Fields);err!=nil{return nil,err}
		items=append(items,item)
	}
	return items,rows.Err()
}

func (r *PostgresDataPlatformRepository) LatestSchema(organizationID,connectionID int64)(model.DatasetSchemaVersion,error){
	var item model.DatasetSchemaVersion; var raw []byte
	err:=r.db.QueryRow(`
		SELECT id,organization_id,connection_id,version,compatibility,status,fields,created_by_user_id,created_at,deprecated_at
		FROM data_platform_schema_versions WHERE organization_id=$1 AND connection_id=$2 AND status='active'
		ORDER BY version DESC LIMIT 1
	`,organizationID,connectionID).Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Version,&item.Compatibility,&item.Status,&raw,&item.CreatedByUserID,&item.CreatedAt,&item.DeprecatedAt)
	if errors.Is(err,sql.ErrNoRows){return model.DatasetSchemaVersion{},ErrDatasetSchemaNotFound}
	if err!=nil{return item,err}
	if err=json.Unmarshal(raw,&item.Fields);err!=nil{return item,err}
	return item,nil
}

func (r *PostgresDataPlatformRepository) DeprecateSchema(organizationID,schemaID int64,now time.Time)error{
	res,err:=r.db.Exec(`UPDATE data_platform_schema_versions SET status='deprecated',deprecated_at=$3 WHERE organization_id=$1 AND id=$2`,organizationID,schemaID,now)
	if err!=nil{return err}; n,_:=res.RowsAffected(); if n==0{return ErrDatasetSchemaNotFound}; return nil
}

func (r *PostgresDataPlatformRepository) CreateExportJob(item model.DataExportJob)(model.DataExportJob,error){
	err:=r.db.QueryRow(`
		INSERT INTO data_export_jobs (
			organization_id,connection_id,mode,status,schema_version,requested_by_user_id,rows,bytes,estimated_cost_usd,
			delivery_uri,payload_hash,checkpoint_before,checkpoint_after,error,started_at,finished_at,created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id,organization_id,connection_id,mode,status,schema_version,requested_by_user_id,rows,bytes,estimated_cost_usd,
		          delivery_uri,payload_hash,checkpoint_before,checkpoint_after,error,started_at,finished_at,created_at
	`,item.OrganizationID,item.ConnectionID,item.Mode,item.Status,item.SchemaVersion,item.RequestedByUserID,item.Rows,item.Bytes,item.EstimatedCostUSD,
		item.DeliveryURI,item.PayloadHash,item.CheckpointBefore,item.CheckpointAfter,item.Error,item.StartedAt,item.FinishedAt,item.CreatedAt).
		Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Mode,&item.Status,&item.SchemaVersion,&item.RequestedByUserID,&item.Rows,&item.Bytes,&item.EstimatedCostUSD,
			&item.DeliveryURI,&item.PayloadHash,&item.CheckpointBefore,&item.CheckpointAfter,&item.Error,&item.StartedAt,&item.FinishedAt,&item.CreatedAt)
	return item,err
}

func (r *PostgresDataPlatformRepository) CompleteExportJob(item model.DataExportJob)(model.DataExportJob,error){
	err:=r.db.QueryRow(`
		UPDATE data_export_jobs SET status=$3,rows=$4,bytes=$5,estimated_cost_usd=$6,delivery_uri=$7,payload_hash=$8,
			checkpoint_before=$9,checkpoint_after=$10,error=$11,finished_at=$12
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,connection_id,mode,status,schema_version,requested_by_user_id,rows,bytes,estimated_cost_usd,
		          delivery_uri,payload_hash,checkpoint_before,checkpoint_after,error,started_at,finished_at,created_at
	`,item.OrganizationID,item.ID,item.Status,item.Rows,item.Bytes,item.EstimatedCostUSD,item.DeliveryURI,item.PayloadHash,
		item.CheckpointBefore,item.CheckpointAfter,item.Error,item.FinishedAt).
		Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Mode,&item.Status,&item.SchemaVersion,&item.RequestedByUserID,&item.Rows,&item.Bytes,&item.EstimatedCostUSD,
			&item.DeliveryURI,&item.PayloadHash,&item.CheckpointBefore,&item.CheckpointAfter,&item.Error,&item.StartedAt,&item.FinishedAt,&item.CreatedAt)
	if errors.Is(err,sql.ErrNoRows){return model.DataExportJob{},ErrDataExportJobNotFound}
	return item,err
}

func (r *PostgresDataPlatformRepository) ListExportJobs(organizationID,connectionID int64,limit int)([]model.DataExportJob,error){
	q:=`
		SELECT id,organization_id,connection_id,mode,status,schema_version,requested_by_user_id,rows,bytes,estimated_cost_usd,
		       delivery_uri,payload_hash,checkpoint_before,checkpoint_after,error,started_at,finished_at,created_at
		FROM data_export_jobs WHERE organization_id=$1 AND ($2=0 OR connection_id=$2) ORDER BY id DESC LIMIT $3
	`
	rows,err:=r.db.Query(q,organizationID,connectionID,limit); if err!=nil{return nil,err}; defer rows.Close()
	items:=[]model.DataExportJob{}
	for rows.Next(){var item model.DataExportJob
		if err:=rows.Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Mode,&item.Status,&item.SchemaVersion,&item.RequestedByUserID,&item.Rows,&item.Bytes,&item.EstimatedCostUSD,
			&item.DeliveryURI,&item.PayloadHash,&item.CheckpointBefore,&item.CheckpointAfter,&item.Error,&item.StartedAt,&item.FinishedAt,&item.CreatedAt);err!=nil{return nil,err}
		items=append(items,item)
	}
	return items,rows.Err()
}

func (r *PostgresDataPlatformRepository) GetCheckpoint(organizationID,connectionID int64)(model.DataExportCheckpoint,error){
	var item model.DataExportCheckpoint
	err:=r.db.QueryRow(`
		SELECT organization_id,connection_id,version,last_updated_at,last_record_key,last_job_id,updated_at
		FROM data_export_checkpoints WHERE organization_id=$1 AND connection_id=$2
	`,organizationID,connectionID).Scan(&item.OrganizationID,&item.ConnectionID,&item.Version,&item.LastUpdatedAt,&item.LastRecordKey,&item.LastJobID,&item.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows){return model.DataExportCheckpoint{},ErrDataExportCheckpointNotFound}; return item,err
}

func (r *PostgresDataPlatformRepository) UpsertCheckpoint(item model.DataExportCheckpoint)(model.DataExportCheckpoint,error){
	err:=r.db.QueryRow(`
		INSERT INTO data_export_checkpoints (organization_id,connection_id,version,last_updated_at,last_record_key,last_job_id,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(connection_id) DO UPDATE SET version=EXCLUDED.version,last_updated_at=EXCLUDED.last_updated_at,
			last_record_key=EXCLUDED.last_record_key,last_job_id=EXCLUDED.last_job_id,updated_at=EXCLUDED.updated_at
		RETURNING organization_id,connection_id,version,last_updated_at,last_record_key,last_job_id,updated_at
	`,item.OrganizationID,item.ConnectionID,item.Version,item.LastUpdatedAt,item.LastRecordKey,item.LastJobID,item.UpdatedAt).
		Scan(&item.OrganizationID,&item.ConnectionID,&item.Version,&item.LastUpdatedAt,&item.LastRecordKey,&item.LastJobID,&item.UpdatedAt)
	return item,err
}

func (r *PostgresDataPlatformRepository) CreateLineage(item model.DataLineageRecord)(model.DataLineageRecord,error){
	src,_:=json.Marshal(item.SourceDatasets); fields,_:=json.Marshal(item.Fields); tags,_:=json.Marshal(item.GovernanceTags)
	var srcOut,fieldsOut,tagsOut []byte
	err:=r.db.QueryRow(`
		INSERT INTO data_lineage_records (organization_id,connection_id,export_job_id,source_datasets,target_dataset,fields,governance_tags,created_at)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6::jsonb,$7::jsonb,$8)
		RETURNING id,organization_id,connection_id,export_job_id,source_datasets,target_dataset,fields,governance_tags,created_at
	`,item.OrganizationID,item.ConnectionID,item.ExportJobID,string(src),item.TargetDataset,string(fields),string(tags),item.CreatedAt).
		Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.ExportJobID,&srcOut,&item.TargetDataset,&fieldsOut,&tagsOut,&item.CreatedAt)
	if err!=nil{return model.DataLineageRecord{},err}
	if err=json.Unmarshal(srcOut,&item.SourceDatasets);err!=nil{return item,err}
	if err=json.Unmarshal(fieldsOut,&item.Fields);err!=nil{return item,err}
	if err=json.Unmarshal(tagsOut,&item.GovernanceTags);err!=nil{return item,err}
	return item,nil
}

func (r *PostgresDataPlatformRepository) ListLineage(organizationID int64,limit int)([]model.DataLineageRecord,error){
	rows,err:=r.db.Query(`
		SELECT id,organization_id,connection_id,export_job_id,source_datasets,target_dataset,fields,governance_tags,created_at
		FROM data_lineage_records WHERE organization_id=$1 ORDER BY id DESC LIMIT $2
	`,organizationID,limit); if err!=nil{return nil,err}; defer rows.Close()
	items:=[]model.DataLineageRecord{}
	for rows.Next(){var item model.DataLineageRecord; var src,fields,tags []byte
		if err:=rows.Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.ExportJobID,&src,&item.TargetDataset,&fields,&tags,&item.CreatedAt);err!=nil{return nil,err}
		if err:=json.Unmarshal(src,&item.SourceDatasets);err!=nil{return nil,err}
		if err:=json.Unmarshal(fields,&item.Fields);err!=nil{return nil,err}
		if err:=json.Unmarshal(tags,&item.GovernanceTags);err!=nil{return nil,err}
		items=append(items,item)
	}
	return items,rows.Err()
}

func (r *PostgresDataPlatformRepository) CreateReverseETLHook(item model.ReverseETLHook)(model.ReverseETLHook,error){
	mapping,_:=json.Marshal(item.FieldMapping); var out []byte
	err:=r.db.QueryRow(`
		INSERT INTO reverse_etl_hooks (organization_id,connection_id,name,source_object,destination,field_mapping,enabled,created_by_user_id,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)
		RETURNING id,organization_id,connection_id,name,source_object,destination,field_mapping,enabled,created_by_user_id,created_at,updated_at
	`,item.OrganizationID,item.ConnectionID,item.Name,item.SourceObject,item.Destination,string(mapping),item.Enabled,item.CreatedByUserID,item.CreatedAt,item.UpdatedAt).
		Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Name,&item.SourceObject,&item.Destination,&out,&item.Enabled,&item.CreatedByUserID,&item.CreatedAt,&item.UpdatedAt)
	if err!=nil{return model.ReverseETLHook{},err}; if err=json.Unmarshal(out,&item.FieldMapping);err!=nil{return item,err}; return item,nil
}

func (r *PostgresDataPlatformRepository) ListReverseETLHooks(organizationID int64)([]model.ReverseETLHook,error){
	rows,err:=r.db.Query(`
		SELECT id,organization_id,connection_id,name,source_object,destination,field_mapping,enabled,created_by_user_id,created_at,updated_at
		FROM reverse_etl_hooks WHERE organization_id=$1 ORDER BY id
	`,organizationID); if err!=nil{return nil,err}; defer rows.Close()
	items:=[]model.ReverseETLHook{}
	for rows.Next(){var item model.ReverseETLHook; var raw []byte
		if err:=rows.Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.Name,&item.SourceObject,&item.Destination,&raw,&item.Enabled,&item.CreatedByUserID,&item.CreatedAt,&item.UpdatedAt);err!=nil{return nil,err}
		if err:=json.Unmarshal(raw,&item.FieldMapping);err!=nil{return nil,err}; items=append(items,item)
	}
	return items,rows.Err()
}
