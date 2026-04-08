package patterns

var ContainerEscape = Category{
	Name:        "container_escape",
	Description: "Potential container escape or host access",
	Patterns: []string{
		"--privileged", "host_pid", "hostPID",
		"/mnt/host", "hostPath:",
		"/etc/shadow", "/etc/passwd",
		"docker.sock", "/var/run/docker",
		"--net=host", "network_mode: host",
		"SYS_ADMIN", "SYS_PTRACE",
		"securityContext:", "allowPrivilegeEscalation",
	},
}
