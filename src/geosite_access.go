package main

import (
	"context"
	"fmt"

	"yigate/proxy"
)

// geoSiteAccess 把当前运行时中的 GeoSite 更新器暴露给管理 API。
// 更新器在每次配置热加载时重建，因此这里按请求解析，避免持有过期实例。
type geoSiteAccess struct {
	s *Server
}

func (g geoSiteAccess) updater() *proxy.GeoSiteUpdater {
	if g.s == nil {
		return nil
	}
	if state := g.s.currentRuntime(); state != nil {
		return state.geoUpdate
	}
	return nil
}

func (g geoSiteAccess) Info() proxy.GeoSiteInfo {
	if updater := g.updater(); updater != nil {
		return updater.Info()
	}
	return proxy.GeoSiteInfo{}
}

func (g geoSiteAccess) DatPath() string {
	if updater := g.updater(); updater != nil {
		return updater.DatPath()
	}
	return ""
}

func (g geoSiteAccess) SyncConfig() proxy.GeoSiteSyncConfig {
	if updater := g.updater(); updater != nil {
		return updater.SyncConfig()
	}
	return proxy.GeoSiteSyncConfig{}
}

// RotateSyncToken 生成新的只读同步令牌并写回配置文件。
// 令牌由系统生成，前端只负责触发、掩码展示与复制。
func (g geoSiteAccess) RotateSyncToken(ctx context.Context) (string, error) {
	if g.s == nil || g.s.cfgPath == "" {
		return "", fmt.Errorf("config path is not available")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return newConfigEditor(g.s.cfgPath, g.s.reloadConfig).RotateGeoSiteSyncToken()
}

func (g geoSiteAccess) Update(ctx context.Context) (proxy.GeoSiteInfo, bool, error) {
	updater := g.updater()
	if updater == nil {
		return proxy.GeoSiteInfo{}, false, fmt.Errorf("geosite updater is not ready")
	}
	changed, err := updater.Sync(ctx)
	if err != nil {
		return updater.Info(), false, err
	}
	if changed && g.s.cfgPath != "" {
		// 与自动更新保持一致：数据变化后重新加载配置，重建 GeoSite 匹配器。
		if reloadErr := g.s.reloadConfig(); reloadErr != nil {
			return g.Info(), true, fmt.Errorf("geosite updated but reload failed: %w", reloadErr)
		}
	}
	return g.Info(), changed, nil
}
