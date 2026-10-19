// Package database manages named database instances with separate data per version.
package database

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type Engine struct {
	Name           string `json:"name"`
	Image          string `json:"image"`
	Port           int    `json:"port"`
	DefaultVersion string `json:"default_version"`
	Client         string `json:"client"`
	Server         string `json:"server"`
	DataMount      string `json:"-"`
}

var Engines = []Engine{
	{"postgres", "docker.io/library/postgres", 5432, "17", "psql", "postgres", "/var/lib/postgresql"},
	{"mysql", "docker.io/library/mysql", 3306, "8.4", "mysql", "mysqld", "/var/lib/mysql"},
	{"mariadb", "docker.io/library/mariadb", 3306, "11.4", "mariadb", "mariadbd", "/var/lib/mysql"},
	{"redis", "docker.io/library/redis", 6379, "7.4", "redis-cli", "redis-server", "/data"},
	{"valkey", "docker.io/valkey/valkey", 6379, "8", "valkey-cli", "valkey-server", "/data"},
}

func engine(name string) (Engine, error) {
	for _, e := range Engines {
		if e.Name == name {
			return e, nil
		}
	}
	return Engine{}, fmt.Errorf("不支持的数据库 %q（postgres/mysql/mariadb/redis/valkey）", name)
}
func serverNames(name string) []string {
	e, _ := engine(name)
	if name == "mariadb" {
		return []string{e.Server, "mysqld"}
	}
	return []string{e.Server}
}

type Request struct {
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	Backend string `json:"backend"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}
type Version struct {
	ID            string `json:"id"`
	ImageID       string `json:"image_id,omitempty"`
	Initialized   bool   `json:"initialized"`
	Allocated     bool   `json:"allocated"`
	ActualVersion string `json:"actual_version,omitempty"`
}
type Instance struct {
	Schema   int                 `json:"schema_version"`
	Name     string              `json:"name"`
	Engine   string              `json:"engine"`
	Backend  string              `json:"backend"`
	Version  string              `json:"version"`
	Port     int                 `json:"port"`
	Owner    string              `json:"owner"`
	Versions map[string]*Version `json:"versions"`
}
type Status struct {
	Instance
	Running      bool   `json:"running"`
	DataLocation string `json:"data_location"`
	Error        string `json:"error,omitempty"`
}
type Connection struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	URI      string `json:"uri"`
}
type credential struct {
	Password string `json:"password"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,95}$`)
var dockerVersionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,3}(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var ownerPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func validate(i Instance) error {
	if i.Schema != 1 || !namePattern.MatchString(i.Name) || !ownerPattern.MatchString(i.Owner) {
		return fmt.Errorf("数据库实例配置无效")
	}
	if _, err := engine(i.Engine); err != nil {
		return err
	}
	if i.Backend != "docker" && i.Backend != "native" {
		return fmt.Errorf("运行方式必须为 docker 或 native")
	}
	if i.Port < 1 || i.Port > 65535 {
		return fmt.Errorf("端口必须在 1–65535 之间")
	}
	if !versionPattern.MatchString(i.Version) || strings.Contains(i.Version, "..") {
		return fmt.Errorf("无效数据库版本")
	}
	if i.Backend == "docker" && !dockerVersionPattern.MatchString(i.Version) {
		return fmt.Errorf("Docker 版本需为明确数字标签，如 17、8.4 或 7.4-alpine")
	}
	if len(i.Versions) > 128 {
		return fmt.Errorf("单实例最多保留 128 个版本")
	}
	for key, v := range i.Versions {
		if v == nil || key != v.ID || !versionPattern.MatchString(key) || strings.Contains(key, "..") || (v.ImageID != "" && !digestPattern.MatchString(v.ImageID)) {
			return fmt.Errorf("数据库版本记录无效")
		}
		if i.Backend == "docker" && v.Allocated && !digestPattern.MatchString(v.ImageID) {
			return fmt.Errorf("已有数据库卷缺少锁定镜像，拒绝重新拉取后复用数据")
		}
	}
	if i.Versions[i.Version] == nil {
		return fmt.Errorf("缺少当前数据库版本记录")
	}
	return nil
}

type driver interface {
	running(context.Context, *Instance) (bool, error)
	start(context.Context, *Instance) error
	stop(context.Context, *Instance) error
	logs(context.Context, *Instance) (string, error)
	exec(context.Context, *Instance, []string) (string, error)
}
