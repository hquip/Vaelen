package database

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processIdentity(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(data), ")")
	if end < 0 {
		return "", fmt.Errorf("无法读取数据库进程状态")
	}
	fields := strings.Fields(string(data)[end+1:])
	if len(fields) < 20 {
		return "", fmt.Errorf("进程状态不完整")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return "", nil
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}
