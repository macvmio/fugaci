package sshrunner

import (
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	"time"
)

type options struct {
	config            *ssh.ClientConfig
	env               []v1.EnvVar
	attachIO          AttachIO
	isSensitiveEnvVar func(name string) bool
	prefix            string
}

type Option func(opts *options)

func WithEnv(env []v1.EnvVar) Option {
	return func(opts *options) {
		opts.env = env
	}
}

func WithIsSensitiveEnvVar(f func(name string) bool) Option {
	return func(opts *options) {
		opts.isSensitiveEnvVar = f
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(opts *options) {
		opts.config.Timeout = timeout
	}
}

func WithAttachIO(attachIO AttachIO) Option {
	return func(opts *options) {
		opts.attachIO = attachIO
	}
}

func WithPrefix(prefix string) Option {
	return func(opts *options) {
		opts.prefix = prefix
	}
}

func makeOptions(dialInfo DialInfo, opts ...Option) *options {
	res := options{
		config: &ssh.ClientConfig{
			User: dialInfo.Username,
			Auth: []ssh.AuthMethod{
				ssh.Password(dialInfo.Password),
			},
			Timeout:         5 * time.Second,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		env:               make([]v1.EnvVar, 0),
		isSensitiveEnvVar: func(name string) bool { return false },
		attachIO:          nil,
	}
	for _, o := range opts {
		o(&res)
	}
	return &res
}
