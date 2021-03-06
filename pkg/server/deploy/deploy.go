package deploy

import (
	"fmt"
	"io"
	"strings"

	log "github.com/Sirupsen/logrus"
	context "golang.org/x/net/context"

	"github.com/luizalabs/teresa/pkg/server/app"
	"github.com/luizalabs/teresa/pkg/server/auth"
	"github.com/luizalabs/teresa/pkg/server/database"
	"github.com/luizalabs/teresa/pkg/server/exec"
	"github.com/luizalabs/teresa/pkg/server/spec"
	st "github.com/luizalabs/teresa/pkg/server/storage"
	"github.com/luizalabs/teresa/pkg/server/teresa_errors"
	"github.com/luizalabs/teresa/pkg/server/uid"
)

const (
	ProcfileReleaseCmd = "release"
	runLabel           = "run"
)

type Operations interface {
	Deploy(ctx context.Context, user *database.User, appName string, tarBall io.ReadSeeker, description string) (io.ReadCloser, <-chan error)
	List(user *database.User, appName string) ([]*ReplicaSetListItem, error)
	Rollback(user *database.User, appName, revision string) error
}

type K8sOperations interface {
	CreateOrUpdateDeploy(deploySpec *spec.Deploy) error
	CreateOrUpdateCronJob(cronJobSpec *spec.CronJob) error
	ExposeDeploy(namespace, name, vHost string, w io.Writer) error
	ReplicaSetListByLabel(namespace, label, value string) ([]*ReplicaSetListItem, error)
	DeployRollbackToRevision(namespace, name, revision string) error
}

type DeployOperations struct {
	appOps      app.Operations
	fileStorage st.Storage
	k8s         K8sOperations
	execOps     exec.Operations
	opts        *Options
}

func (ops *DeployOperations) Deploy(ctx context.Context, user *database.User, appName string, tarBall io.ReadSeeker, description string) (io.ReadCloser, <-chan error) {
	errChan := make(chan error, 1)
	a, err := ops.appOps.Get(appName)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	teamName, err := ops.appOps.TeamName(appName)
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	a.Team = teamName

	if !ops.appOps.HasPermission(user, appName) {
		errChan <- auth.ErrPermissionDenied
		return nil, errChan
	}

	confFiles, err := getDeployConfigFilesFromTarBall(tarBall, a.ProcessType)
	if err != nil {
		errChan <- teresa_errors.New(ErrInvalidTeresaYamlFile, err)
		return nil, errChan
	}

	deployId := uid.New()
	buildDest := fmt.Sprintf("deploys/%s/%s/out", appName, deployId)

	r, w := io.Pipe()
	go func() {
		defer w.Close()
		if err = ops.buildApp(ctx, tarBall, a, deployId, buildDest, w); err != nil {
			errChan <- err
			log.WithError(err).WithField("id", deployId).Errorf("Building app %s", appName)
			return
		}

		slugURL := fmt.Sprintf("%s/slug.tgz", buildDest)
		if a.ProcessType == app.ProcessTypeCron {
			ops.createOrUpdateCronJob(a, confFiles, w, errChan, slugURL, description)
		} else {
			ops.createOrUpdateDeploy(a, confFiles, w, errChan, slugURL, description, deployId)
		}
	}()
	return r, errChan
}

func (ops *DeployOperations) runReleaseCmd(a *app.App, deployId, slugURL string, stream io.Writer) error {
	imgs := &spec.SlugImages{
		Runner: ops.opts.SlugRunnerImage,
		Store:  ops.opts.SlugStoreImage,
	}
	podSpec := spec.NewRunner(
		fmt.Sprintf("release-%s-%s", a.Name, deployId),
		slugURL,
		imgs,
		a,
		ops.fileStorage,
		ops.buildLimits(),
		"start",
		ProcfileReleaseCmd,
	)

	fmt.Fprintln(stream, "Running release command")
	if err := ops.podRun(context.Background(), podSpec, stream); err != nil {
		if err == ErrPodRunFail {
			return ErrReleaseFail
		}
		return err
	}
	return nil
}

func (ops *DeployOperations) buildLimits() *spec.ContainerLimits {
	return &spec.ContainerLimits{
		CPU:    ops.opts.BuildLimitCPU,
		Memory: ops.opts.BuildLimitMemory,
	}
}

func (ops *DeployOperations) createOrUpdateDeploy(a *app.App, confFiles *DeployConfigFiles, w io.Writer, errChan chan error, slugURL, description, deployId string) {
	releaseCmd := confFiles.Procfile[ProcfileReleaseCmd]
	if confFiles.Procfile != nil && releaseCmd != "" {
		if err := ops.runReleaseCmd(a, deployId, slugURL, w); err != nil {
			errChan <- err
			log.WithError(err).WithField("id", deployId).Errorf("Running release command %s in app %s", releaseCmd, a.Name)
			return
		}
	}

	imgs := &spec.SlugImages{
		Runner: ops.opts.SlugRunnerImage,
		Store:  ops.opts.SlugStoreImage,
	}
	deploySpec := spec.NewDeploy(
		imgs,
		description,
		slugURL,
		ops.opts.RevisionHistoryLimit,
		a,
		confFiles.TeresaYaml,
		ops.fileStorage,
	)

	if err := ops.k8s.CreateOrUpdateDeploy(deploySpec); err != nil {
		errChan <- err
		log.WithError(err).Errorf("Creating deploy app %s", a.Name)
		return
	}

	if err := ops.exposeApp(a, w); err != nil {
		errChan <- err
		log.WithError(err).Errorf("Exposing service %s", a.Name)
	} else {
		fmt.Fprintln(w, fmt.Sprintf("The app %s has been successfully deployed", a.Name))
	}
}

func (ops *DeployOperations) createOrUpdateCronJob(a *app.App, confFiles *DeployConfigFiles, w io.Writer, errChan chan error, slugURL, description string) {
	imgs := &spec.SlugImages{
		Runner: ops.opts.SlugRunnerImage,
		Store:  ops.opts.SlugStoreImage,
	}
	cronSpec := spec.NewCronJob(
		description,
		slugURL,
		confFiles.TeresaYaml.Cron.Schedule,
		imgs,
		a,
		ops.fileStorage,
		strings.Split(confFiles.Procfile[app.ProcessTypeCron], " ")...,
	)

	if err := ops.k8s.CreateOrUpdateCronJob(cronSpec); err != nil {
		errChan <- err
		log.WithError(err).Errorf("Creating CronJob %s", a.Name)
	} else {
		fmt.Fprintln(w, fmt.Sprintf("The CronJob %s has been successfully deployed", a.Name))
	}
}

func (ops *DeployOperations) exposeApp(a *app.App, w io.Writer) error {
	if a.ProcessType != app.ProcessTypeWeb {
		return nil
	}
	if err := ops.k8s.ExposeDeploy(a.Name, a.Name, a.VirtualHost, w); err != nil {
		return err
	}
	return nil // already exposed
}

func (ops *DeployOperations) buildApp(ctx context.Context, tarBall io.ReadSeeker, a *app.App, deployId, buildDest string, stream io.Writer) error {
	tarBall.Seek(0, 0)
	tarBallLocation := fmt.Sprintf("deploys/%s/%s/in/app.tar.gz", a.Name, deployId)
	if err := ops.fileStorage.UploadFile(tarBallLocation, tarBall); err != nil {
		fmt.Fprintln(stream, "The Deploy failed to upload the tarBall to slug storage")
		return err
	}

	podSpec := spec.NewBuilder(
		fmt.Sprintf("build-%s", deployId),
		tarBallLocation,
		buildDest,
		ops.opts.SlugBuilderImage,
		a,
		ops.fileStorage,
		ops.buildLimits(),
	)

	if err := ops.podRun(ctx, podSpec, stream); err != nil {
		if err == ErrPodRunFail {
			return ErrBuildFail
		}
		return err
	}
	return nil
}

func (ops *DeployOperations) podRun(ctx context.Context, podSpec *spec.Pod, stream io.Writer) error {
	podStream, runErrChan := ops.execOps.RunCommandBySpec(ctx, podSpec)
	go io.Copy(stream, podStream)

	if err := <-runErrChan; err != nil {
		return ErrPodRunFail
	}

	return nil
}

func (ops *DeployOperations) List(user *database.User, appName string) ([]*ReplicaSetListItem, error) {
	if _, err := ops.appOps.Get(appName); err != nil {
		return nil, err
	}

	if !ops.appOps.HasPermission(user, appName) {
		return nil, auth.ErrPermissionDenied
	}

	items, err := ops.k8s.ReplicaSetListByLabel(appName, runLabel, appName)
	if err != nil {
		return nil, teresa_errors.NewInternalServerError(err)
	}

	return items, nil
}

func (ops *DeployOperations) Rollback(user *database.User, appName, revision string) error {
	app, err := ops.appOps.CheckPermAndGet(user, appName)
	if err != nil {
		return err
	}

	if err = ops.k8s.DeployRollbackToRevision(appName, appName, revision); err != nil {
		return teresa_errors.NewInternalServerError(err)
	}

	if err := ops.appOps.SaveApp(app, user.Email); err != nil {
		return teresa_errors.NewInternalServerError(err)
	}

	return nil
}

func NewDeployOperations(aOps app.Operations, k8s K8sOperations, s st.Storage, execOps exec.Operations, opts *Options) Operations {
	return &DeployOperations{
		appOps:      aOps,
		k8s:         k8s,
		fileStorage: s,
		execOps:     execOps,
		opts:        opts,
	}
}
