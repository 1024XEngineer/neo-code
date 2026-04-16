//go:build !windows

package config

// PersistUserEnvVar persists a key/value pair into user-level environment storage.
// On non-Windows platforms, NeoCode currently relies on .env persistence and process env.
func PersistUserEnvVar(key string, value string) error {
	return nil
}

// DeleteUserEnvVar 删除用户级环境变量；非 Windows 平台当前无需额外处理。
func DeleteUserEnvVar(key string) error {
	return nil
}

// LookupUserEnvVar 查询用户级环境变量；非 Windows 平台当前无单独存储，统一返回不存在。
func LookupUserEnvVar(key string) (string, bool, error) {
	return "", false, nil
}
