package models

// ─── User Roles ──────────────────────────────────────────────────────

const (
	UserRoleUser       = "user"
	UserRoleAdmin      = "admin"
	UserRoleSuperAdmin = "super_admin"
	UserRoleDeveloper  = "developer"
)

// ─── Workspace Member Roles ──────────────────────────────────────────

const (
	WorkspaceMemberRoleOwner  = "owner"
	WorkspaceMemberRoleAdmin  = "admin"
	WorkspaceMemberRoleMember = "member"
	WorkspaceMemberRoleViewer = "viewer"
)

// ─── File Types ──────────────────────────────────────────────────────

const (
	FileTypeFolder = "folder"
	FileTypeVideo  = "video"
	FileTypeImage  = "image"
	FileTypeOther  = "other"
	FileTypeSpace  = "space"
)

// ─── File Statuses ───────────────────────────────────────────────────

const (
	FileStatusWaiting    = "waiting"
	FileStatusProcessing = "processing"
	FileStatusReady      = "ready"
	FileStatusError      = "error"
)

// ─── File Source Types ───────────────────────────────────────────────

const (
	FileSourceTypeUpload  = "upload"
	FileSourceTypeYoutube = "youtube"
	FileSourceTypeVimeo   = "vimeo"
	FileSourceTypeOther   = "other"
)

// ─── Media Types ─────────────────────────────────────────────────────

const (
	MediaTypeVideo     = "video"
	MediaTypeAudio     = "audio"
	MediaTypeSubtitle  = "subtitle"
	MediaTypeThumbnail = "thumbnail"
	MediaTypeImage     = "image"
	MediaTypeDocument  = "document"
	MediaTypeOther     = "other"
)

// ─── Ingest Source Types ─────────────────────────────────────────────

const (
	IngestSourceTypeUpload   = "upload"
	IngestSourceTypeRemote   = "remote"
	IngestSourceTypeGDrive   = "gdrive"
	IngestSourceTypeS3Import = "s3_import"
)

// ─── Storage Types ───────────────────────────────────────────────────

const (
	StorageTypeLocal = "local"
	StorageTypeS3    = "s3"
)

// ─── Storage Statuses ────────────────────────────────────────────────

const (
	StorageStatusOnline      = "online"
	StorageStatusOffline     = "offline"
	StorageStatusError       = "error"
	StorageStatusMaintenance = "maintenance"
)

// ─── Storage Accepts ─────────────────────────────────────────────────

const (
	StorageAcceptUpload = "upload"
	StorageAcceptVideo  = "video"
	StorageAcceptImage  = "image"
	StorageAcceptOther  = "other"
)

// ─── Resolution ──────────────────────────────────────────────────────

const (
	ResolutionOriginal = "original"
)

// ─── Domain Statuses ─────────────────────────────────────────────────

const (
	DomainStatusPending = "pending"
	DomainStatusActive  = "active"
	DomainStatusFailed  = "failed"
	DomainStatusExpired = "expired"
)

// ─── DMCA Statuses ───────────────────────────────────────────────────

const (
	DmcaStatusPending       = "pending"
	DmcaStatusReviewing     = "reviewing"
	DmcaStatusApproved      = "approved"
	DmcaStatusRejected      = "rejected"
	DmcaStatusCounterNotice = "counter_notice"
)

// ─── DMCA Types ──────────────────────────────────────────────────────

const (
	DmcaTypeCopyright = "copyright"
	DmcaTypeTrademark = "trademark"
	DmcaTypeOther     = "other"
)
