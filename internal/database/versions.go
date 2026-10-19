package database

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var reportedVersion = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

func checkReportedVersion(wanted string, output []byte) (string, error) {
	actual := reportedVersion.FindString(string(output))
	if actual == "" {
		return "", fmt.Errorf("服务端没有返回可识别的版本信息")
	}
	if dockerVersionPattern.MatchString(wanted) {
		prefix := strings.Split(wanted, "-")[0]
		if actual != prefix && !strings.HasPrefix(actual, prefix+".") {
			return "", fmt.Errorf("请求版本 %s，程序实际为 %s", wanted, actual)
		}
	}
	return actual, nil
}
func (d nativeDriver) verify(ctx context.Context, i *Instance) error {
	e, _ := engine(i.Engine)
	names := []string{e.Server}
	if i.Engine == "mariadb" {
		names = append(names, "mysqld")
	}
	output, err := d.call(ctx, i, names, []string{"--version"}, nil)
	if err != nil {
		return err
	}
	actual, err := checkReportedVersion(i.Version, output)
	if err != nil {
		return err
	}
	v := i.Versions[i.Version]
	if v.Allocated && v.ActualVersion != "" && v.ActualVersion != actual {
		return fmt.Errorf("原生程序由 %s 变为 %s，拒绝复用旧数据；请使用新的版本标识", v.ActualVersion, actual)
	}
	v.ActualVersion = actual
	return nil
}
