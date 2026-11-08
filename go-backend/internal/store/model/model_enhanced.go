package model

// ─── myflvx enhanced tables (subscriptions & node deployment) ────────
//
// GORM omits zero-valued fields that carry a `default` tag on INSERT, so every
// default below equals the Go zero value and callers always set the real value.

// ProxyNode is a proxy node managed in 代理节点. Config holds the Clash/mihomo
// proxy definition as JSON and is the source of truth; the other columns are
// denormalised for listing and filtering. Every node belongs to one user and is
// only visible to that user.
type ProxyNode struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	UserID      int64  `gorm:"column:user_id;not null;default:0;index"`
	Name        string `gorm:"type:varchar(200);not null"`
	Protocol    string `gorm:"type:varchar(32);not null;default:''"`
	Server      string `gorm:"type:varchar(255);not null;default:''"`
	Port        int    `gorm:"not null;default:0"`
	Config      string `gorm:"type:text;not null"`
	RawURL      string `gorm:"column:raw_url;type:text;not null;default:''"`
	Tags        string `gorm:"type:text;not null;default:''"`
	Remark      string `gorm:"type:text;not null;default:''"`
	Source      string `gorm:"type:varchar(32);not null;default:''"`
	SourceRef   string `gorm:"column:source_ref;type:varchar(255);not null;default:''"`
	ParentID    int64  `gorm:"column:parent_id;not null;default:0;index"`
	TunnelID    int64  `gorm:"column:tunnel_id;not null;default:0"`
	ForwardID   int64  `gorm:"column:forward_id;not null;default:0;index"`
	RelayHost   string `gorm:"column:relay_host;type:varchar(255);not null;default:''"`
	RelayEntry  int64  `gorm:"column:relay_entry;not null;default:0"`
	Enabled     int    `gorm:"not null;default:0"`
	Inx         int    `gorm:"not null;default:0"`
	CreatedTime int64  `gorm:"column:created_time;not null"`
	UpdatedTime int64  `gorm:"column:updated_time;not null"`
}

func (ProxyNode) TableName() string { return "proxy_node" }

// SubFile is a subscription in 订阅管理: an uploaded or imported Clash
// configuration, or one generated from the owner's nodes on every download.
// Subscriptions belong to one user and are never shared.
type SubFile struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	UserID       int64  `gorm:"column:user_id;not null;default:0;index"`
	Name         string `gorm:"type:varchar(200);not null"`
	Remark       string `gorm:"type:text;not null;default:''"`
	Type         string `gorm:"type:varchar(20);not null"`
	SourceURL    string `gorm:"column:source_url;type:text;not null;default:''"`
	UserAgent    string `gorm:"column:user_agent;type:varchar(255);not null;default:''"`
	Content      string `gorm:"type:text;not null"`
	GenConfig    string `gorm:"column:gen_config;type:text;not null"`
	FileName     string `gorm:"column:file_name;type:varchar(200);not null;default:''"`
	SubInfo      string `gorm:"column:sub_info;type:varchar(255);not null;default:''"`
	AutoUpdate   int    `gorm:"column:auto_update;not null;default:0"`
	Enabled      int    `gorm:"not null;default:0"`
	ExpireTime   int64  `gorm:"column:expire_time;not null;default:0"`
	LastSyncTime int64  `gorm:"column:last_sync_time;not null;default:0"`
	Inx          int    `gorm:"not null;default:0"`
	CreatedTime  int64  `gorm:"column:created_time;not null"`
	UpdatedTime  int64  `gorm:"column:updated_time;not null"`
}

func (SubFile) TableName() string { return "sub_file" }

// ProxyDeploy is a mihomo_script deployment (部署节点) on one FLVX node. The
// panel runs the script through the node agent; Params holds the JSON encoded
// proxykit.DeployParams. TaskID/TaskOp identify the agent task in progress so
// polling can resume after a panel restart.
type ProxyDeploy struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	UserID      int64  `gorm:"column:user_id;not null;default:0"`
	Name        string `gorm:"type:varchar(200);not null"`
	NodeID      int64  `gorm:"column:node_id;not null;default:0"`
	Host        string `gorm:"type:varchar(255);not null;default:''"`
	Protocols   string `gorm:"type:varchar(100);not null;default:''"`
	Params      string `gorm:"type:text;not null"`
	Status      string `gorm:"type:varchar(20);not null;default:''"`
	TaskID      string `gorm:"column:task_id;type:varchar(64);not null;default:''"`
	TaskOp      string `gorm:"column:task_op;type:varchar(20);not null;default:''"`
	LastLog     string `gorm:"column:last_log;type:text;not null"`
	LastRunTime int64  `gorm:"column:last_run_time;not null;default:0"`
	Remark      string `gorm:"type:text;not null;default:''"`
	Inx         int    `gorm:"not null;default:0"`
	CreatedTime int64  `gorm:"column:created_time;not null"`
	UpdatedTime int64  `gorm:"column:updated_time;not null"`
}

func (ProxyDeploy) TableName() string { return "proxy_deploy" }

// ─── Backup structures for the enhanced tables ───────────────────────

type ProxyNodeBackup struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId,omitempty"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	Server      string `json:"server"`
	Port        int    `json:"port"`
	Config      string `json:"config"`
	RawURL      string `json:"rawUrl,omitempty"`
	Tags        string `json:"tags,omitempty"`
	Remark      string `json:"remark,omitempty"`
	Source      string `json:"source"`
	SourceRef   string `json:"sourceRef,omitempty"`
	ParentID    int64  `json:"parentId,omitempty"`
	TunnelID    int64  `json:"tunnelId,omitempty"`
	ForwardID   int64  `json:"forwardId,omitempty"`
	RelayHost   string `json:"relayHost,omitempty"`
	RelayEntry  int64  `json:"relayEntry,omitempty"`
	Enabled     int    `json:"enabled"`
	Inx         int    `json:"inx"`
	CreatedTime int64  `json:"createdTime"`
	UpdatedTime int64  `json:"updatedTime"`
}

type SubFileBackup struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"userId,omitempty"`
	Name         string `json:"name"`
	Remark       string `json:"remark,omitempty"`
	Type         string `json:"type"`
	SourceURL    string `json:"sourceUrl,omitempty"`
	UserAgent    string `json:"userAgent,omitempty"`
	Content      string `json:"content,omitempty"`
	GenConfig    string `json:"genConfig,omitempty"`
	FileName     string `json:"fileName,omitempty"`
	SubInfo      string `json:"subInfo,omitempty"`
	AutoUpdate   int    `json:"autoUpdate,omitempty"`
	Enabled      int    `json:"enabled"`
	ExpireTime   int64  `json:"expireTime,omitempty"`
	LastSyncTime int64  `json:"lastSyncTime,omitempty"`
	Inx          int    `json:"inx"`
	CreatedTime  int64  `json:"createdTime"`
	UpdatedTime  int64  `json:"updatedTime"`
	// UserIDs is the subscription assignment of myflvx 2.2.1. Subscriptions are
	// per user now; the field is accepted (and ignored) so old backups import.
	UserIDs []int64 `json:"userIds,omitempty"`
}

type ProxyDeployBackup struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId,omitempty"`
	Name        string `json:"name"`
	NodeID      int64  `json:"nodeId,omitempty"`
	Host        string `json:"host"`
	Protocols   string `json:"protocols"`
	Params      string `json:"params"`
	Status      string `json:"status"`
	LastRunTime int64  `json:"lastRunTime,omitempty"`
	Remark      string `json:"remark,omitempty"`
	Inx         int    `json:"inx,omitempty"`
	CreatedTime int64  `json:"createdTime"`
	UpdatedTime int64  `json:"updatedTime"`
}
