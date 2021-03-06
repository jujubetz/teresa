package spec

import (
	"testing"

	"github.com/luizalabs/teresa/pkg/server/app"
	"github.com/luizalabs/teresa/pkg/server/storage"
)

func TestNewPodSpec(t *testing.T) {
	expectedAppName := "app-test"
	a := &app.App{
		Name: expectedAppName,
		EnvVars: []*app.EnvVar{
			{Key: "APP-ENV-KEY", Value: "APP-ENV-VALUE"},
		},
	}
	ev := map[string]string{"ENV-KEY": "ENV-VALUE"}
	expectedName := "test"
	expectedImage := "docker/teresa-test:0.0.1"

	ps := NewPod(expectedName, expectedImage, a, ev, storage.NewFake())
	if ps.Name != expectedName {
		t.Errorf("expected %s, got %s", expectedName, ps.Name)
	}
	if ps.Namespace != a.Name {
		t.Errorf("expected %s, got %s", a.Name, ps.Namespace)
	}
	if ps.Image != expectedImage {
		t.Errorf("expected %s, got %s", expectedImage, ps.Image)
	}

	for _, env := range a.EnvVars {
		if ps.Env[env.Key] != env.Value {
			t.Errorf("expected %s, got %s for key %s", env.Value, ps.Env[env.Key], env.Key)
		}
	}

	for k, v := range ev {
		if ps.Env[k] != v {
			t.Errorf("expected %s, got %s for key %s", v, ps.Env[k], k)
		}
	}

	if len(ps.Volumes) != 2 {
		t.Errorf("expected %d, got %d", 2, len(ps.Volumes))
	}
	if len(ps.VolumeMounts) != 0 {
		t.Errorf("expected %d, got %d", 0, len(ps.VolumeMounts))
	}
	if ps.Volumes[0].Name != "storage-keys" {
		t.Errorf("expected %s, got %s", "storage-keys", ps.Volumes[0].Name)
	}
	if ps.Volumes[1].Name != slugVolumeName {
		t.Errorf("expected %s, got %s", slugVolumeName, ps.Volumes[1].Name)
	}
}

func TestNewBuilder(t *testing.T) {
	expectedName := "builder"
	expectedTarBallLocation := "narnia"
	expectedBuildDest := "nowhere"
	expectedImage := "image"
	expectedContainerLimits := &ContainerLimits{
		CPU:    "800m",
		Memory: "1Gi",
	}

	ps := NewBuilder(
		expectedName,
		expectedTarBallLocation,
		expectedBuildDest,
		expectedImage,
		&app.App{},
		storage.NewFake(),
		expectedContainerLimits,
	)

	if ps.Name != expectedName {
		t.Errorf("expected %s, got %s", expectedName, ps.Name)
	}
	if ps.Image != expectedImage {
		t.Errorf("expected %s, got %s", expectedImage, ps.Image)
	}

	ev := map[string]string{
		"TAR_PATH": expectedTarBallLocation,
		"PUT_PATH": expectedBuildDest,
	}
	for k, v := range ev {
		if ps.Env[k] != v {
			t.Errorf("expected %s, got %s for key %s", v, ps.Env[k], k)
		}
	}

	if ps.ContainerLimits.CPU != expectedContainerLimits.CPU {
		t.Errorf("expected %s, got %s", expectedContainerLimits.CPU, ps.ContainerLimits.CPU)
	}
	if ps.ContainerLimits.Memory != expectedContainerLimits.Memory {
		t.Errorf("expected %s, got %s", expectedContainerLimits.Memory, ps.ContainerLimits.Memory)
	}

	if len(ps.VolumeMounts) != 1 {
		t.Errorf("expected 1, got %d", len(ps.VolumeMounts))
	}
	if ps.VolumeMounts[0].Name != "storage-keys" {
		t.Errorf("expected %s, got %s", "storage-keys", ps.VolumeMounts[0].Name)
	}
}

func TestNewRunner(t *testing.T) {
	expectedPodName := "1234"
	expectedSlugURL := "http://teresa.io/slug.tgz"
	expectedImage := "image"
	expectedCommand := []string{"python", "manage.py", "migrate"}
	a := &app.App{}
	s := storage.NewFake()
	expectedContainerLimits := &ContainerLimits{
		CPU:    "800m",
		Memory: "1Gi",
	}
	imgs := &SlugImages{Runner: expectedImage}

	ps := NewRunner(
		expectedPodName,
		expectedSlugURL,
		imgs,
		a,
		s,
		expectedContainerLimits,
		expectedCommand...,
	)
	if ps.Name != expectedPodName {
		t.Errorf("expected %s, got %s", expectedPodName, ps.Name)
	}

	if ps.Image != expectedImage {
		t.Errorf("expected %s, got %s", expectedImage, ps.Image)
	}

	ev := map[string]string{
		"SLUG_URL": expectedSlugURL,
	}
	for k, v := range ev {
		if ps.Env[k] != v {
			t.Errorf("expected %s, got %s for key %s", v, ps.Env[k], k)
		}
	}

	if ps.ContainerLimits.CPU != expectedContainerLimits.CPU {
		t.Errorf("expected %s, got %s", expectedContainerLimits.CPU, ps.ContainerLimits.CPU)
	}
	if ps.ContainerLimits.Memory != expectedContainerLimits.Memory {
		t.Errorf("expected %s, got %s", expectedContainerLimits.Memory, ps.ContainerLimits.Memory)
	}

	for i, v := range expectedCommand {
		if ps.Args[i] != expectedCommand[i] {
			t.Errorf("expected %s, got %s", v, ps.Args[i])
		}
	}
}

func TestNewRunnerInitContainers(t *testing.T) {
	expectedImage := "image"
	cmd := []string{}
	a := &app.App{}
	s := storage.NewFake()
	cl := &ContainerLimits{}
	imgs := &SlugImages{Store: expectedImage}

	ps := NewRunner("", "", imgs, a, s, cl, cmd...)

	if len(ps.InitContainers) != 1 {
		t.Errorf("expected %d, got %d", 1, len(ps.InitContainers))
	}
	if ps.InitContainers[0].Image != expectedImage {
		t.Errorf("expected %s, got %s", expectedImage, ps.InitContainers[0].Image)
	}
}

func TestNewRunnerVolumeMounts(t *testing.T) {
	expectedImage := "image"
	cmd := []string{}
	a := &app.App{}
	s := storage.NewFake()
	cl := &ContainerLimits{}
	imgs := &SlugImages{Store: expectedImage}

	ps := NewRunner("", "", imgs, a, s, cl, cmd...)

	if len(ps.VolumeMounts) != 1 {
		t.Errorf("expected %d, got %d", 1, len(ps.VolumeMounts))
	}
	if ps.VolumeMounts[0].Name != slugVolumeName {
		t.Errorf("expected %s, got %s", slugVolumeName, ps.VolumeMounts[0].Name)
	}
}
