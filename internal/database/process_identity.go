package database

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"vsm/internal/config"
)

type processRecord struct {
	PID   int    `json:"pid"`
	Token string `json:"token"`
}

func (d nativeDriver) recordProcess(i *Instance, pid int) error {
	token, err := processIdentity(pid)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("数据库进程已退出，请查看实例日志")
	}
	data, _ := json.Marshal(processRecord{PID: pid, Token: token})
	return config.WriteFileAtomic(d.m.versionPath(i, "process.json"), data, 0600)
}
func (d nativeDriver) trackedProcess(i *Instance) (bool, error) {
	data, err := config.ReadBounded(d.m.versionPath(i, "process.json"), 4096)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var record processRecord
	if err := config.DecodeStrict(data, &record); err != nil {
		return false, err
	}
	if record.PID <= 0 || strings.TrimSpace(record.Token) == "" {
		return false, fmt.Errorf("数据库进程身份记录损坏")
	}
	current, err := processIdentity(record.PID)
	if err != nil {
		return false, err
	}
	return current != "" && current == record.Token, nil
}
