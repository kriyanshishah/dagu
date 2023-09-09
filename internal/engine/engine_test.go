package engine_test

import (
	"github.com/dagu-dev/dagu/internal/persistence/jsondb"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/dagu-dev/dagu/internal/config"
	"github.com/dagu-dev/dagu/internal/dag"
	"github.com/dagu-dev/dagu/internal/engine"
	"github.com/dagu-dev/dagu/internal/models"
	"github.com/dagu-dev/dagu/internal/scheduler"
	"github.com/dagu-dev/dagu/internal/sock"
	"github.com/dagu-dev/dagu/internal/utils"
	"github.com/stretchr/testify/require"
)

var (
	testdataDir = path.Join(utils.MustGetwd(), "./testdata")
)

func TestMain(m *testing.M) {
	tempDir := utils.MustTempDir("controller_test")
	changeHomeDir(tempDir)
	code := m.Run()
	_ = os.RemoveAll(tempDir)
	os.Exit(code)
}

func changeHomeDir(homeDir string) {
	_ = os.Setenv("HOME", homeDir)
	_ = config.LoadConfig(homeDir)
}

func TestGetStatusRunningAndDone(t *testing.T) {
	file := testDAG("get_status.yaml")

	e := engine.NewFactory().Create()

	ds, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	socketServer, _ := sock.NewServer(
		&sock.Config{
			Addr: ds.DAG.SockAddr(),
			HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
				status := models.NewStatus(
					ds.DAG, []*scheduler.Node{},
					scheduler.SchedulerStatus_Running, 0, nil, nil)
				w.WriteHeader(http.StatusOK)
				b, _ := status.ToJson()
				_, _ = w.Write(b)
			},
		})

	go func() {
		_ = socketServer.Serve(nil)
		_ = socketServer.Shutdown()
	}()

	time.Sleep(time.Millisecond * 100)
	st, err := e.GetStatus(ds.DAG)
	require.NoError(t, err)
	require.Equal(t, scheduler.SchedulerStatus_Running, st.Status)

	_ = socketServer.Shutdown()

	st, err = e.GetStatus(ds.DAG)
	require.NoError(t, err)
	require.Equal(t, scheduler.SchedulerStatus_None, st.Status)
}

func TestGrepDAGs(t *testing.T) {
	e := engine.NewFactory().Create()
	ret, _, err := e.GrepDAG(testdataDir, "aabbcc")
	require.NoError(t, err)
	require.Equal(t, 1, len(ret))

	ret, _, err = e.GrepDAG(testdataDir, "steps")
	require.NoError(t, err)
	require.Greater(t, len(ret), 1)
}

func TestUpdateStatus(t *testing.T) {
	var (
		file      = testDAG("update_status.yaml")
		requestId = "test-update-status"
		now       = time.Now()
	)

	e := engine.NewFactory().Create()
	d, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	hs := jsondb.New()

	err = hs.Open(d.DAG.Location, now, requestId)
	require.NoError(t, err)

	st := testNewStatus(d.DAG, requestId,
		scheduler.SchedulerStatus_Success, scheduler.NodeStatus_Success)

	err = hs.Write(st)
	require.NoError(t, err)
	_ = hs.Close()

	time.Sleep(time.Millisecond * 100)

	st, err = e.GetStatusByRequestId(d.DAG, requestId)
	require.NoError(t, err)
	require.Equal(t, scheduler.NodeStatus_Success, st.Nodes[0].Status)

	newStatus := scheduler.NodeStatus_Error
	st.Nodes[0].Status = newStatus

	err = e.UpdateStatus(d.DAG, st)
	require.NoError(t, err)

	statusByRequestId, err := e.GetStatusByRequestId(d.DAG, requestId)
	require.NoError(t, err)

	require.Equal(t, 1, len(st.Nodes))
	require.Equal(t, newStatus, statusByRequestId.Nodes[0].Status)
}

func TestUpdateStatusError(t *testing.T) {
	var (
		file      = testDAG("update_status_failed.yaml")
		requestId = "test-update-status-failure"
	)

	e := engine.NewFactory().Create()
	d, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	status := testNewStatus(d.DAG, requestId,
		scheduler.SchedulerStatus_Error, scheduler.NodeStatus_Error)

	err = e.UpdateStatus(d.DAG, status)
	require.Error(t, err)

	// update with invalid request id
	status.RequestId = "invalid-request-id"
	err = e.UpdateStatus(d.DAG, status)
	require.Error(t, err)
}

func TestStart(t *testing.T) {
	file := testDAG("start.yaml")
	e := engine.NewFactory().Create()

	d, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	err = e.Start(d.DAG, path.Join(utils.MustGetwd(), "../../bin/dagu"), "", "")
	require.Error(t, err)

	status, err := e.GetLastStatus(d.DAG)
	require.NoError(t, err)
	require.Equal(t, scheduler.SchedulerStatus_Error, status.Status)
}

func TestStop(t *testing.T) {
	file := testDAG("stop.yaml")
	e := engine.NewFactory().Create()

	d, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	e.StartAsync(d.DAG, path.Join(utils.MustGetwd(), "../../bin/dagu"), "", "")

	require.Eventually(t, func() bool {
		st, _ := e.GetStatus(d.DAG)
		return st.Status == scheduler.SchedulerStatus_Running
	}, time.Millisecond*1500, time.Millisecond*100)

	_ = e.Stop(d.DAG)

	require.Eventually(t, func() bool {
		st, _ := e.GetLastStatus(d.DAG)
		return st.Status == scheduler.SchedulerStatus_Cancel
	}, time.Millisecond*1500, time.Millisecond*100)
}

func TestRestart(t *testing.T) {
	file := testDAG("restart.yaml")
	e := engine.NewFactory().Create()

	d, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	err = e.Restart(d.DAG, path.Join(utils.MustGetwd(), "../../bin/dagu"), "")
	require.NoError(t, err)

	status, err := e.GetLastStatus(d.DAG)
	require.NoError(t, err)
	require.Equal(t, scheduler.SchedulerStatus_Success, status.Status)
}

func TestRetry(t *testing.T) {
	file := testDAG("retry.yaml")
	e := engine.NewFactory().Create()

	d, err := e.ReadStatus(file, false)
	require.NoError(t, err)

	err = e.Start(d.DAG, path.Join(utils.MustGetwd(), "../../bin/dagu"), "", "x y z")
	require.NoError(t, err)

	status, err := e.GetLastStatus(d.DAG)
	require.NoError(t, err)
	require.Equal(t, scheduler.SchedulerStatus_Success, status.Status)

	requestId := status.RequestId
	params := status.Params

	err = e.Retry(d.DAG, path.Join(utils.MustGetwd(), "../../bin/dagu"), "", requestId)
	require.NoError(t, err)
	status, err = e.GetLastStatus(d.DAG)
	require.NoError(t, err)

	require.Equal(t, scheduler.SchedulerStatus_Success, status.Status)
	require.Equal(t, params, status.Params)

	statusByRequestId, err := e.GetStatusByRequestId(d.DAG, status.RequestId)
	require.NoError(t, err)
	require.Equal(t, status, statusByRequestId)

	recentStatuses := e.GetRecentStatuses(d.DAG, 1)
	require.Equal(t, status, recentStatuses[0].Status)
}

func TestUpdate(t *testing.T) {
	tmpDir := utils.MustTempDir("engine-test-save")
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	loc := path.Join(tmpDir, "test.yaml")
	d := &dag.DAG{
		Name:     "test",
		Location: loc,
	}
	e := engine.NewFactory().Create()

	// invalid DAG
	invalidDAG := `name: test DAG`
	err := e.UpdateDAGSpec(d, invalidDAG)
	require.Error(t, err)

	// valid DAG
	validDAG := `name: test DAG
steps:
  - name: "1"
    command: "true"
`
	// Update Error: the DAG does not exist
	err = e.UpdateDAGSpec(d, validDAG)
	require.Error(t, err)

	// create a new DAG file
	newFile, _ := utils.CreateFile(loc)
	defer func() {
		_ = newFile.Close()
	}()

	// Update the DAG
	err = e.UpdateDAGSpec(d, validDAG)
	require.NoError(t, err)

	// Check the content of the DAG file
	updatedFile, _ := os.Open(loc)
	defer func() {
		_ = updatedFile.Close()
	}()
	b, err := io.ReadAll(updatedFile)
	require.NoError(t, err)
	require.Equal(t, validDAG, string(b))
}

func TestRemove(t *testing.T) {
	tmpDir := utils.MustTempDir("engine-test-remove")
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	loc := path.Join(tmpDir, "test.yaml")
	d := &dag.DAG{
		Name:     "test",
		Location: loc,
	}

	dagSpec := `name: test DAG
steps:
  - name: "1"
    command: "true"
`
	// create file
	newFile, _ := utils.CreateFile(loc)
	defer func() {
		_ = newFile.Close()
	}()

	e := engine.NewFactory().Create()
	err := e.UpdateDAGSpec(d, dagSpec)
	require.NoError(t, err)

	// check file
	saved, _ := os.Open(loc)
	defer func() {
		_ = saved.Close()
	}()
	b, err := io.ReadAll(saved)
	require.NoError(t, err)
	require.Equal(t, dagSpec, string(b))

	// remove file
	err = e.DeleteDAG(d)
	require.NoError(t, err)
	require.NoFileExists(t, loc)
}

func TestCreateNewDAG(t *testing.T) {
	tmpDir := utils.MustTempDir("engine-test-save")
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// invalid filename
	filename := path.Join(tmpDir, "test")
	e := engine.NewFactory().Create()
	err := e.CreateDAG(filename)
	require.Error(t, err)

	// valid filename
	filename = path.Join(tmpDir, "test.yaml")
	err = e.CreateDAG(filename)
	require.NoError(t, err)

	// check file is created
	cl := &dag.Loader{}

	d, err := cl.Load(filename, "")
	require.NoError(t, err)
	require.Equal(t, "test", d.Name)

	steps := d.Steps[0]
	require.Equal(t, "step1", steps.Name)
	require.Equal(t, "echo", steps.Command)
	require.Equal(t, []string{"hello"}, steps.Args)
}

func TestRenameDAG(t *testing.T) {
	tmpDir := utils.MustTempDir("engine-test-rename")
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	oldName := path.Join(tmpDir, "rename_dag.yaml")
	newName := path.Join(tmpDir, "rename_dag_renamed.yaml")

	e := engine.NewFactory().Create()
	err := e.CreateDAG(oldName)
	require.NoError(t, err)

	_, err = e.ReadStatus(oldName, false)
	require.NoError(t, err)

	err = e.MoveDAG(oldName, "invalid-config-name")
	require.Error(t, err)

	err = e.MoveDAG(oldName, newName)
	require.NoError(t, err)
	require.FileExists(t, newName)
}

func TestLoadConfig(t *testing.T) {
	file := testDAG("invalid_dag.yaml")
	e := engine.NewFactory().Create()

	d, err := e.ReadStatus(file, false)
	require.Error(t, err)
	require.NotNil(t, d)

	// contains error message
	require.Error(t, d.Error)
}

func TestReadAll(t *testing.T) {
	e := engine.NewFactory().Create()
	dags, _, err := e.ReadAllStatus(testdataDir)
	require.NoError(t, err)
	require.Greater(t, len(dags), 0)

	pattern := path.Join(testdataDir, "*.yaml")
	matches, err := filepath.Glob(pattern)
	require.NoError(t, err)
	if len(matches) != len(dags) {
		t.Fatalf("unexpected number of dags: %d", len(dags))
	}
}

func TestReadDAGStatus(t *testing.T) {
	file := testDAG("read_status.yaml")
	e := engine.NewFactory().Create()

	_, err := e.ReadStatus(file, false)
	require.NoError(t, err)
}

func testDAG(name string) string {
	return path.Join(testdataDir, name)
}

func testNewStatus(d *dag.DAG, reqId string, status scheduler.SchedulerStatus, nodeStatus scheduler.NodeStatus) *models.Status {
	now := time.Now()
	ret := models.NewStatus(
		d, []*scheduler.Node{{NodeState: scheduler.NodeState{Status: nodeStatus}}},
		status, 0, &now, nil)
	ret.RequestId = reqId
	return ret
}