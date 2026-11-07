package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"go-backend/internal/store/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ─── myflvx: proxy nodes ─────────────────────────────────────────────

func (r *Repository) ListProxyNodes() ([]model.ProxyNode, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.ProxyNode
	err := r.db.Order("inx ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *Repository) GetProxyNode(id int64) (*model.ProxyNode, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var node model.ProxyNode
	err := r.db.Where("id = ?", id).First(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *Repository) GetProxyNodesByIDs(ids []int64) ([]model.ProxyNode, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	if len(ids) == 0 {
		return []model.ProxyNode{}, nil
	}
	var nodes []model.ProxyNode
	err := r.db.Where("id IN ?", ids).Order("inx ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *Repository) ListProxyNodesBySource(source, sourceRef string) ([]model.ProxyNode, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.ProxyNode
	err := r.db.Where("source = ? AND source_ref = ?", source, sourceRef).Order("inx ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *Repository) ListProxyNodeChildren(parentID int64) ([]model.ProxyNode, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.ProxyNode
	err := r.db.Where("parent_id = ?", parentID).Order("inx ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *Repository) ListProxyNodesByForward(forwardID int64) ([]model.ProxyNode, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.ProxyNode
	err := r.db.Where("forward_id = ?", forwardID).Order("inx ASC, id ASC").Find(&nodes).Error
	return nodes, err
}

func (r *Repository) nextProxyNodeInx(tx *gorm.DB) int {
	var max sql.NullInt64
	tx.Model(&model.ProxyNode{}).Select("MAX(inx)").Scan(&max)
	if !max.Valid {
		return 0
	}
	return int(max.Int64) + 1
}

// CreateProxyNodes inserts nodes, appending them after the existing ones.
func (r *Repository) CreateProxyNodes(nodes []*model.ProxyNode) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(nodes) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		inx := r.nextProxyNodeInx(tx)
		for _, n := range nodes {
			n.Inx = inx
			inx++
			if err := tx.Create(n).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) SaveProxyNode(node *model.ProxyNode) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Save(node).Error
}

func (r *Repository) DeleteProxyNodes(ids []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Where("id IN ?", ids).Delete(&model.ProxyNode{}).Error
}

func (r *Repository) SetProxyNodesEnabled(ids []int64, enabled int, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.ProxyNode{}).Where("id IN ?", ids).
		Updates(map[string]interface{}{"enabled": enabled, "updated_time": now}).Error
}

func (r *Repository) SetProxyNodesTags(ids []int64, tags string, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.ProxyNode{}).Where("id IN ?", ids).
		Updates(map[string]interface{}{"tags": tags, "updated_time": now}).Error
}

func (r *Repository) UpdateProxyNodeOrder(order map[int64]int) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for id, inx := range order {
			if err := tx.Model(&model.ProxyNode{}).Where("id = ?", id).Update("inx", inx).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ─── myflvx: subscriptions ───────────────────────────────────────────

func (r *Repository) ListSubFiles() ([]model.SubFile, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.SubFile
	err := r.db.Order("inx ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *Repository) GetSubFile(id int64) (*model.SubFile, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.SubFile
	err := r.db.Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) CreateSubFile(item *model.SubFile, userIDs []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var max sql.NullInt64
		tx.Model(&model.SubFile{}).Select("MAX(inx)").Scan(&max)
		if max.Valid {
			item.Inx = int(max.Int64) + 1
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		return replaceSubFileUsers(tx, item.ID, userIDs, item.CreatedTime)
	})
}

func (r *Repository) SaveSubFile(item *model.SubFile, userIDs []int64, replaceUsers bool) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		if replaceUsers {
			return replaceSubFileUsers(tx, item.ID, userIDs, item.UpdatedTime)
		}
		return nil
	})
}

func (r *Repository) UpdateSubFileSync(id int64, content, subInfo, fileName string, syncTime int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	updates := map[string]interface{}{"content": content, "sub_info": subInfo, "last_sync_time": syncTime, "updated_time": syncTime}
	if fileName != "" {
		updates["file_name"] = fileName
	}
	return r.db.Model(&model.SubFile{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) UpdateSubFileCache(id int64, content string) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.SubFile{}).Where("id = ?", id).Update("content", content).Error
}

func (r *Repository) DeleteSubFiles(ids []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sub_file_id IN ?", ids).Delete(&model.SubFileUser{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&model.SubFile{}).Error
	})
}

func (r *Repository) UpdateSubFileOrder(order map[int64]int) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for id, inx := range order {
			if err := tx.Model(&model.SubFile{}).Where("id = ?", id).Update("inx", inx).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func replaceSubFileUsers(tx *gorm.DB, subFileID int64, userIDs []int64, now int64) error {
	if err := tx.Where("sub_file_id = ?", subFileID).Delete(&model.SubFileUser{}).Error; err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, uid := range userIDs {
		if uid <= 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		row := model.SubFileUser{SubFileID: subFileID, UserID: uid, CreatedTime: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// SubFileUserMap returns subscription id → assigned user ids.
func (r *Repository) SubFileUserMap() (map[int64][]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.SubFileUser
	if err := r.db.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int64][]int64{}
	for _, row := range rows {
		out[row.SubFileID] = append(out[row.SubFileID], row.UserID)
	}
	return out, nil
}

// ListSubFilesForUser returns the enabled subscriptions assigned to a user.
func (r *Repository) ListSubFilesForUser(userID int64) ([]model.SubFile, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.SubFile
	err := r.db.Model(&model.SubFile{}).
		Joins("JOIN sub_file_user ON sub_file_user.sub_file_id = sub_file.id").
		Where("sub_file_user.user_id = ? AND sub_file.enabled = 1", userID).
		Order("sub_file.inx ASC, sub_file.id ASC").
		Find(&items).Error
	return items, err
}

func (r *Repository) UserHasSubFile(userID, subFileID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("repository not initialized")
	}
	var count int64
	err := r.db.Model(&model.SubFileUser{}).Where("user_id = ? AND sub_file_id = ?", userID, subFileID).Count(&count).Error
	return count > 0, err
}

// ListUserBriefs returns id/name/role/status of all users for assignment UIs.
func (r *Repository) ListUserBriefs() ([]map[string]interface{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var users []model.User
	if err := r.db.Select("id, user, role_id, status").Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]interface{}{"id": u.ID, "user": u.User, "roleId": u.RoleID, "status": u.Status})
	}
	return out, nil
}

// ─── myflvx: deployments ─────────────────────────────────────────────

func (r *Repository) ListProxyDeploys() ([]model.ProxyDeploy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.ProxyDeploy
	err := r.db.Order("id DESC").Find(&items).Error
	return items, err
}

func (r *Repository) GetProxyDeploy(id int64) (*model.ProxyDeploy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.ProxyDeploy
	err := r.db.Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) CreateProxyDeploy(item *model.ProxyDeploy) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Create(item).Error
}

func (r *Repository) SaveProxyDeploy(item *model.ProxyDeploy) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Save(item).Error
}

func (r *Repository) UpdateProxyDeployRun(id int64, status, log string, runTime, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	updates := map[string]interface{}{"status": status, "last_log": log, "updated_time": now}
	if runTime > 0 {
		updates["last_run_time"] = runTime
	}
	return r.db.Model(&model.ProxyDeploy{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) DeleteProxyDeploy(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("id = ?", id).Delete(&model.ProxyDeploy{}).Error
}

// ─── myflvx: forward helpers for relay nodes ─────────────────────────

// ForwardEntryEndpoint is one entry (listen address) of a forward.
type ForwardEntryEndpoint struct {
	NodeID   int64  `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// ListForwardEntryEndpoints returns the client-facing entry addresses of a
// forward, using the same rule as the forward list (listen IP, else the
// entry node's address).
func (r *Repository) ListForwardEntryEndpoints(forwardID int64) ([]ForwardEntryEndpoint, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	type row struct {
		NodeID   int64
		NodeName sql.NullString
		Port     int
		InIP     sql.NullString
		ServerIP sql.NullString
	}
	var rows []row
	err := r.db.Model(&model.ForwardPort{}).
		Select("forward_port.node_id, node.name AS node_name, forward_port.port, forward_port.in_ip, node.server_ip").
		Joins("LEFT JOIN node ON node.id = forward_port.node_id").
		Where("forward_port.forward_id = ?", forwardID).
		Order("forward_port.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ForwardEntryEndpoint, 0, len(rows))
	for _, item := range rows {
		host := ""
		if item.InIP.Valid {
			for _, candidate := range strings.Split(item.InIP.String, ",") {
				candidate = strings.Trim(strings.TrimSpace(candidate), "[]")
				if candidate != "" && candidate != "0.0.0.0" && candidate != "::" {
					host = candidate
					break
				}
			}
		}
		if host == "" && item.ServerIP.Valid {
			host = strings.Trim(strings.TrimSpace(item.ServerIP.String), "[]")
		}
		out = append(out, ForwardEntryEndpoint{NodeID: item.NodeID, NodeName: item.NodeName.String, Host: host, Port: item.Port})
	}
	return out, nil
}

// FindLatestForwardID finds the forward just created through the regular
// forward API (which does not return the new id).
func (r *Repository) FindLatestForwardID(userID, tunnelID int64, name, remoteAddr string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not initialized")
	}
	var f model.Forward
	err := r.db.Where("user_id = ? AND tunnel_id = ? AND name = ? AND remote_addr = ?", userID, tunnelID, name, remoteAddr).
		Order("id DESC").First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return f.ID, err
}

func (r *Repository) GetForwardBrief(forwardID int64) (*model.Forward, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var f model.Forward
	err := r.db.Where("id = ?", forwardID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListForwardBriefs returns all forwards (id, name, tunnel, remote address).
func (r *Repository) ListForwardBriefs() ([]model.Forward, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.Forward
	err := r.db.Select("id, user_id, user_name, name, tunnel_id, remote_addr, status").Order("inx ASC, id ASC").Find(&items).Error
	return items, err
}

// ─── myflvx: backup of the enhanced tables ───────────────────────────

func (r *Repository) exportProxyNodes() ([]model.ProxyNodeBackup, error) {
	nodes, err := r.ListProxyNodes()
	if err != nil {
		return nil, err
	}
	out := make([]model.ProxyNodeBackup, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, model.ProxyNodeBackup{
			ID: n.ID, Name: n.Name, Protocol: n.Protocol, Server: n.Server, Port: n.Port, Config: n.Config,
			RawURL: n.RawURL, Tags: n.Tags, Remark: n.Remark, Source: n.Source, SourceRef: n.SourceRef,
			ParentID: n.ParentID, TunnelID: n.TunnelID, ForwardID: n.ForwardID, RelayHost: n.RelayHost,
			RelayEntry: n.RelayEntry, Enabled: n.Enabled, Inx: n.Inx, CreatedTime: n.CreatedTime, UpdatedTime: n.UpdatedTime,
		})
	}
	return out, nil
}

func (r *Repository) exportSubFiles() ([]model.SubFileBackup, error) {
	items, err := r.ListSubFiles()
	if err != nil {
		return nil, err
	}
	users, err := r.SubFileUserMap()
	if err != nil {
		return nil, err
	}
	out := make([]model.SubFileBackup, 0, len(items))
	for _, s := range items {
		out = append(out, model.SubFileBackup{
			ID: s.ID, Name: s.Name, Remark: s.Remark, Type: s.Type, SourceURL: s.SourceURL, UserAgent: s.UserAgent,
			Content: s.Content, GenConfig: s.GenConfig, FileName: s.FileName, SubInfo: s.SubInfo,
			AutoUpdate: s.AutoUpdate, Enabled: s.Enabled, ExpireTime: s.ExpireTime, LastSyncTime: s.LastSyncTime,
			Inx: s.Inx, CreatedTime: s.CreatedTime, UpdatedTime: s.UpdatedTime, UserIDs: users[s.ID],
		})
	}
	return out, nil
}

func (r *Repository) exportProxyDeploys() ([]model.ProxyDeployBackup, error) {
	items, err := r.ListProxyDeploys()
	if err != nil {
		return nil, err
	}
	out := make([]model.ProxyDeployBackup, 0, len(items))
	for _, d := range items {
		status := d.Status
		if status == "running" {
			status = "failed"
		}
		out = append(out, model.ProxyDeployBackup{
			ID: d.ID, Name: d.Name, NodeID: d.NodeID, Host: d.Host, Protocols: d.Protocols, Params: d.Params,
			Status: status, LastRunTime: d.LastRunTime, Remark: d.Remark, CreatedTime: d.CreatedTime, UpdatedTime: d.UpdatedTime,
		})
	}
	return out, nil
}

// exportEnhanced fills the enhanced sections of a backup for the selected types.
func (r *Repository) exportEnhanced(backup *model.BackupData, typeSet map[string]bool) error {
	if typeSet["proxyNodes"] {
		v, err := r.exportProxyNodes()
		if err != nil {
			return fmt.Errorf("export proxy nodes failed: %w", err)
		}
		backup.ProxyNodes = v
	}
	if typeSet["subscriptions"] {
		v, err := r.exportSubFiles()
		if err != nil {
			return fmt.Errorf("export subscriptions failed: %w", err)
		}
		backup.Subscriptions = v
	}
	if typeSet["proxyDeploys"] {
		v, err := r.exportProxyDeploys()
		if err != nil {
			return fmt.Errorf("export deployments failed: %w", err)
		}
		backup.ProxyDeploys = v
	}
	return nil
}

// importEnhanced upserts the enhanced sections of a backup by id.
func importEnhanced(tx *gorm.DB, backup *model.BackupData, typeSet map[string]bool, now int64, result *model.ImportResult) error {
	if typeSet["proxyNodes"] && len(backup.ProxyNodes) > 0 {
		for _, n := range backup.ProxyNodes {
			item := model.ProxyNode{
				ID: n.ID, Name: n.Name, Protocol: n.Protocol, Server: n.Server, Port: n.Port, Config: n.Config,
				RawURL: n.RawURL, Tags: n.Tags, Remark: n.Remark, Source: n.Source, SourceRef: n.SourceRef,
				ParentID: n.ParentID, TunnelID: n.TunnelID, ForwardID: n.ForwardID, RelayHost: n.RelayHost,
				RelayEntry: n.RelayEntry, Enabled: n.Enabled, Inx: n.Inx, CreatedTime: n.CreatedTime, UpdatedTime: now,
			}
			if item.CreatedTime == 0 {
				item.CreatedTime = now
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(&item).Error; err != nil {
				return fmt.Errorf("import proxy nodes failed: %w", err)
			}
			result.ProxyNodesImported++
		}
	}
	if typeSet["subscriptions"] && len(backup.Subscriptions) > 0 {
		for _, s := range backup.Subscriptions {
			item := model.SubFile{
				ID: s.ID, Name: s.Name, Remark: s.Remark, Type: s.Type, SourceURL: s.SourceURL, UserAgent: s.UserAgent,
				Content: s.Content, GenConfig: s.GenConfig, FileName: s.FileName, SubInfo: s.SubInfo,
				AutoUpdate: s.AutoUpdate, Enabled: s.Enabled, ExpireTime: s.ExpireTime, LastSyncTime: s.LastSyncTime,
				Inx: s.Inx, CreatedTime: s.CreatedTime, UpdatedTime: now,
			}
			if item.CreatedTime == 0 {
				item.CreatedTime = now
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(&item).Error; err != nil {
				return fmt.Errorf("import subscriptions failed: %w", err)
			}
			if err := replaceSubFileUsers(tx, item.ID, s.UserIDs, now); err != nil {
				return fmt.Errorf("import subscription users failed: %w", err)
			}
			result.SubscriptionsImported++
		}
	}
	if typeSet["proxyDeploys"] && len(backup.ProxyDeploys) > 0 {
		for _, d := range backup.ProxyDeploys {
			item := model.ProxyDeploy{
				ID: d.ID, Name: d.Name, NodeID: d.NodeID, Host: d.Host, Protocols: d.Protocols, Params: d.Params,
				Status: d.Status, LastRunTime: d.LastRunTime, Remark: d.Remark, CreatedTime: d.CreatedTime, UpdatedTime: now,
			}
			if item.Status == "" || item.Status == "running" {
				item.Status = "pending"
			}
			if item.CreatedTime == 0 {
				item.CreatedTime = now
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(&item).Error; err != nil {
				return fmt.Errorf("import deployments failed: %w", err)
			}
			result.ProxyDeploysImported++
		}
	}
	return nil
}

// ListTunnelNames returns tunnel id → name.
func (r *Repository) ListTunnelNames() (map[int64]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.Tunnel
	if err := r.db.Select("id, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(rows))
	for _, t := range rows {
		out[t.ID] = t.Name
	}
	return out, nil
}
