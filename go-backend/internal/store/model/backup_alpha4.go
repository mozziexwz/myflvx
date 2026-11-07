package model

// Alpha4BackupData mirrors the backup schema of FLVX 2.2.0-alpha4, copied
// verbatim from that release. Exporting through these structs keeps only the
// fields alpha4 knows, because its importer rejects unknown JSON fields.
// Do not add fields here.

type Alpha4BackupData struct {
	Version      string              `json:"version"`
	ExportedAt   int64               `json:"exportedAt"`
	Users        []Alpha4UserBackup        `json:"users,omitempty"`
	Nodes        []Alpha4NodeBackup        `json:"nodes,omitempty"`
	Tunnels      []Alpha4TunnelBackup      `json:"tunnels,omitempty"`
	Forwards     []Alpha4ForwardBackup     `json:"forwards,omitempty"`
	UserTunnels  []Alpha4UserTunnelBackup  `json:"userTunnels,omitempty"`
	SpeedLimits  []Alpha4SpeedLimitBackup  `json:"speedLimits,omitempty"`
	TunnelGroups []Alpha4TunnelGroupBackup `json:"tunnelGroups,omitempty"`
	UserGroups   []Alpha4UserGroupBackup   `json:"userGroups,omitempty"`
	Permissions  []Alpha4PermissionBackup  `json:"permissions,omitempty"`
	Configs      map[string]string   `json:"configs,omitempty"`
}

type Alpha4UserBackup struct {
	ID              int64  `json:"id"`
	User            string `json:"user"`
	Pwd             string `json:"pwd"`
	RoleID          int    `json:"roleId"`
	ExpTime         int64  `json:"expTime"`
	Flow            int64  `json:"flow"`
	InFlow          int64  `json:"inFlow"`
	OutFlow         int64  `json:"outFlow"`
	FlowResetTime   int64  `json:"flowResetTime"`
	DailyQuotaGB    int64  `json:"dailyQuotaGB,omitempty"`
	MonthlyQuotaGB  int64  `json:"monthlyQuotaGB,omitempty"`
	DisabledByQuota int    `json:"disabledByQuota,omitempty"`
	QuotaDisabledAt int64  `json:"quotaDisabledAt,omitempty"`
	Num             int    `json:"num"`
	CreatedTime     int64  `json:"createdTime"`
	UpdatedTime     int64  `json:"updatedTime,omitempty"`
	Status          int    `json:"status"`
}

type Alpha4NodeBackup struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Remark        string `json:"remark,omitempty"`
	ExpiryTime    int64  `json:"expiryTime,omitempty"`
	RenewalCycle  string `json:"renewalCycle,omitempty"`
	Secret        string `json:"secret"`
	ServerIP      string `json:"serverIp"`
	ServerIPv4    string `json:"serverIpV4,omitempty"`
	ServerIPv6    string `json:"serverIpV6,omitempty"`
	ExtraIPs      string `json:"extraIPs,omitempty"`
	Port          string `json:"port"`
	InterfaceName string `json:"interfaceName,omitempty"`
	Version       string `json:"version,omitempty"`
	HTTP          int    `json:"http"`
	TLS           int    `json:"tls"`
	Socks         int    `json:"socks"`
	CreatedTime   int64  `json:"createdTime"`
	UpdatedTime   int64  `json:"updatedTime,omitempty"`
	Status        int    `json:"status"`
	TCPListenAddr string `json:"tcpListenAddr"`
	UDPListenAddr string `json:"udpListenAddr"`
	Inx           int    `json:"inx"`
	IsRemote      int    `json:"isRemote"`
	RemoteURL     string `json:"remoteUrl,omitempty"`
	RemoteToken   string `json:"remoteToken,omitempty"`
	RemoteConfig  string `json:"remoteConfig,omitempty"`
}

type Alpha4TunnelBackup struct {
	ID           int64               `json:"id"`
	Name         string              `json:"name"`
	TrafficRatio float64             `json:"trafficRatio"`
	Type         int                 `json:"type"`
	Protocol     string              `json:"protocol"`
	Flow         int64               `json:"flow"`
	CreatedTime  int64               `json:"createdTime"`
	UpdatedTime  int64               `json:"updatedTime"`
	Status       int                 `json:"status"`
	InIP         string              `json:"inIp,omitempty"`
	Inx          int                 `json:"inx"`
	IPPreference string              `json:"ipPreference,omitempty"`
	ChainTunnels []Alpha4ChainTunnelBackup `json:"chainTunnels,omitempty"`
}

type Alpha4ChainTunnelBackup struct {
	ID        int64  `json:"id"`
	TunnelID  int64  `json:"tunnelId"`
	ChainType string `json:"chainType"`
	NodeID    int64  `json:"nodeId"`
	Port      int    `json:"port,omitempty"`
	Strategy  string `json:"strategy,omitempty"`
	Inx       int    `json:"inx,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type Alpha4ForwardBackup struct {
	ID           int64                `json:"id"`
	UserID       int64                `json:"userId"`
	UserName     string               `json:"userName"`
	Name         string               `json:"name"`
	TunnelID     int64                `json:"tunnelId"`
	RemoteAddr   string               `json:"remoteAddr"`
	Strategy     string               `json:"strategy"`
	InFlow       int64                `json:"inFlow"`
	OutFlow      int64                `json:"outFlow"`
	CreatedTime  int64                `json:"createdTime"`
	UpdatedTime  int64                `json:"updatedTime"`
	Status       int                  `json:"status"`
	Inx          int                  `json:"inx"`
	SpeedID      *int64               `json:"speedId,omitempty"`
	ForwardPorts *[]Alpha4ForwardPortBackup `json:"forwardPorts,omitempty"`
}

type Alpha4ForwardPortBackup struct {
	NodeID int64 `json:"nodeId"`
	Port   int   `json:"port"`
}

type Alpha4UserTunnelBackup struct {
	ID            int64 `json:"id"`
	UserID        int64 `json:"userId"`
	TunnelID      int64 `json:"tunnelId"`
	SpeedID       int64 `json:"speedId,omitempty"`
	Num           int   `json:"num"`
	Flow          int64 `json:"flow"`
	InFlow        int64 `json:"inFlow"`
	OutFlow       int64 `json:"outFlow"`
	FlowResetTime int64 `json:"flowResetTime"`
	ExpTime       int64 `json:"expTime"`
	Status        int   `json:"status"`
}

type Alpha4SpeedLimitBackup struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Speed       int64  `json:"speed"`
	TunnelID    *int64 `json:"tunnelId,omitempty"`
	TunnelName  string `json:"tunnelName,omitempty"`
	CreatedTime int64  `json:"createdTime"`
	UpdatedTime int64  `json:"updatedTime,omitempty"`
	Status      int    `json:"status"`
}

type Alpha4TunnelGroupBackup struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	CreatedTime int64   `json:"createdTime"`
	UpdatedTime int64   `json:"updatedTime"`
	Status      int     `json:"status"`
	Tunnels     []int64 `json:"tunnels,omitempty"`
}

type Alpha4UserGroupBackup struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	CreatedTime int64   `json:"createdTime"`
	UpdatedTime int64   `json:"updatedTime"`
	Status      int     `json:"status"`
	Users       []int64 `json:"users,omitempty"`
}

type Alpha4PermissionBackup struct {
	ID             int64                   `json:"id"`
	UserGroupID    int64                   `json:"userGroupId"`
	TunnelGroupID  int64                   `json:"tunnelGroupId"`
	CreatedTime    int64                   `json:"createdTime"`
	CreatedByGroup int                     `json:"createdByGroup"`
	Grants         []Alpha4PermissionGrantBackup `json:"grants,omitempty"`
}

type Alpha4PermissionGrantBackup struct {
	ID             int64 `json:"id"`
	UserGroupID    int64 `json:"userGroupId"`
	TunnelGroupID  int64 `json:"tunnelGroupId"`
	UserTunnelID   int64 `json:"userTunnelId"`
	CreatedTime    int64 `json:"createdTime"`
	CreatedByGroup int   `json:"createdByGroup"`
}
