// Package build provides a pipe that can build binaries for several
// languages.
package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goreleaser/goreleaser/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/goreleaser/goreleaser/internal/semerrgroup"
	"github.com/goreleaser/goreleaser/internal/tmpl"
	builders "github.com/goreleaser/goreleaser/pkg/build"
	"github.com/goreleaser/goreleaser/pkg/config"
	"github.com/goreleaser/goreleaser/pkg/context"

	// langs to init
	_ "github.com/goreleaser/goreleaser/internal/builders/golang"
)

// Pipe for build
type Pipe struct{}

func (Pipe) String() string {
	return "building binaries"
}

// Run the pipe
func (Pipe) Run(ctx *context.Context) error {
	for _, build := range ctx.Config.Builds {
		log.WithField("build", build).Debug("building")
		if err := runPipeOnBuild(ctx, build); err != nil {
			return err
		}
	}
	return nil
}

// Default sets the pipe defaults
func (Pipe) Default(ctx *context.Context) error {
	for i, build := range ctx.Config.Builds {
		ctx.Config.Builds[i] = buildWithDefaults(ctx, build)
	}
	if len(ctx.Config.Builds) == 0 {
		ctx.Config.Builds = []config.Build{
			buildWithDefaults(ctx, ctx.Config.SingleBuild),
		}
	}
	if len(ctx.Config.Builds) > 1 {
		log.Warn("you have more than 1 build setup: please make sure it is a not a typo on your config")
	}
	return nil
}

func buildWithDefaults(ctx *context.Context, build config.Build) config.Build {
	if build.Lang == "" {
		build.Lang = "go"
	}
	if build.Binary == "" {
		build.Binary = ctx.Config.ProjectName
	}
	for k, v := range build.Env {
		build.Env[k] = os.ExpandEnv(v)
	}
	return builders.For(build.Lang).WithDefaults(build)
}

func runPipeOnBuild(ctx *context.Context, build config.Build) error {
	var op errors.Op = "build.Run"
	if err := runHook(ctx, build.Env, build.Hooks.Pre); err != nil {
		return errors.E(op, err, errors.KindBuildError)
	}
	var g = semerrgroup.New(ctx.Parallelism)
	for _, target := range build.Targets {
		target := target
		build := build
		g.Go(func() error {
			return doBuild(ctx, build, target)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if err := runHook(ctx, build.Env, build.Hooks.Post); err != nil {
		return errors.E(op, err, errors.KindBuildError)
	}
	return nil
}

func runHook(ctx *context.Context, env []string, hook string) error {
	if hook == "" {
		return nil
	}
	log.WithField("hook", hook).Info("running hook")
	cmd := strings.Fields(hook)
	return run(ctx, cmd, env)
}

func doBuild(ctx *context.Context, build config.Build, target string) error {
	var ext = extFor(target)

	binary, err := tmpl.New(ctx).Apply(build.Binary)
	if err != nil {
		return err
	}

	build.Binary = binary
	var name = build.Binary + ext
	var path = filepath.Join(ctx.Config.Dist, target, name)
	log.WithField("binary", path).Info("building")
	return builders.For(build.Lang).Build(ctx, build, builders.Options{
		Target: target,
		Name:   name,
		Path:   path,
		Ext:    ext,
	})
}

func extFor(target string) string {
	if strings.Contains(target, "windows") {
		return ".exe"
	}
	return ""
}

func run(ctx *context.Context, command, env []string) error {
	var op errors.Op = "build.run"
	/* #nosec */
	var cmd = exec.CommandContext(ctx, command[0], command[1:]...)
	var log = log.WithField("env", env).WithField("cmd", command)
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, env...)
	log.WithField("cmd", command).WithField("env", env).Debug("running")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.WithError(err).Debug("failed")
		return errors.E(op, string(out))
	}
	return nil
}
