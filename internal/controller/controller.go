package controller

import (
	"fmt"
	"io/ioutil"
	"jobctl/internal/config"
	"jobctl/internal/database"
	"jobctl/internal/models"
	"jobctl/internal/scheduler"
	"jobctl/internal/sock"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Controller interface {
	StopJob() error
	StartJob(bin string, workDir string, params string) error
	RetryJob(bin string, workDir string, reqId string) error
	GetStatus() (*models.Status, error)
	GetLastStatus() (*models.Status, error)
	GetStatusHist(n int) ([]*models.StatusFile, error)
}

func GetJobList(dir string) ([]*Job, error) {
	ret := []*Job{}
	fis, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Printf("%v", err)
	}
	for _, fi := range fis {
		if filepath.Ext(fi.Name()) != ".yaml" {
			continue
		}
		job, err := fromConfig(filepath.Join(dir, fi.Name()), true)
		if err != nil {
			log.Printf("%v", err)
			if job == nil {
				continue
			}
		}
		ret = append(ret, job)
	}
	return ret, nil
}

var _ Controller = (*controller)(nil)

type controller struct {
	cfg *config.Config
}

func New(cfg *config.Config) Controller {
	return &controller{
		cfg: cfg,
	}
}

func (c *controller) StopJob() error {
	unixClient, err := sock.NewUnixClient(sock.GetSockAddr(c.cfg.ConfigPath))
	if err != nil {
		return err
	}
	_, err = unixClient.Request("POST", "/stop")
	return err
}

func (c *controller) StartJob(bin string, workDir string, params string) (err error) {
	go func() {
		args := []string{"start"}
		if params != "" {
			args = append(args, fmt.Sprintf("--params=\"%s\"", params))
		}
		args = append(args, c.cfg.ConfigPath)
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
		cmd.Dir = workDir
		cmd.Env = os.Environ()
		defer cmd.Wait()
		err = cmd.Start()
		if err != nil {
			log.Printf("failed to start a job: %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)
	return
}

func (c *controller) RetryJob(bin string, workDir string, reqId string) (err error) {
	log.Printf("retry start: %s, %s, %s, %s", bin, workDir, c.cfg.ConfigPath, reqId)
	go func() {
		args := []string{"retry"}
		args = append(args, fmt.Sprintf("--req=%s", reqId))
		args = append(args, c.cfg.ConfigPath)
		cmd := exec.Command(bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
		cmd.Dir = workDir
		cmd.Env = os.Environ()
		defer cmd.Wait()
		err := cmd.Start()
		if err != nil {
			log.Printf("failed to retry a job: %v", err)
		}
	}()
	time.Sleep(time.Millisecond * 500)
	return
}

func (s *controller) GetStatus() (*models.Status, error) {
	unixClient, err := sock.NewUnixClient(sock.GetSockAddr(s.cfg.ConfigPath))
	if err != nil {
		return nil, err
	}
	ret, err := unixClient.Request("GET", "/status")
	if err != nil {
		return defaultStatus(s.cfg), nil
	}
	status, err := models.StatusFromJson(ret)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (s *controller) GetLastStatus() (*models.Status, error) {
	unixClient, err := sock.NewUnixClient(sock.GetSockAddr(s.cfg.ConfigPath))
	if err != nil {
		return nil, err
	}
	ret, err := unixClient.Request("GET", "/status")
	if err == nil {
		return models.StatusFromJson(ret)
	}
	db := database.New(database.DefaultConfig())
	status, err := db.ReadStatusToday(s.cfg.ConfigPath)
	if err != nil {
		if err != database.ErrNoDataFile {
			fmt.Printf("read status failed : %s", err)
		}
		return defaultStatus(s.cfg), nil
	}
	return status, nil
}

func (s *controller) GetStatusHist(n int) ([]*models.StatusFile, error) {
	db := database.New(database.DefaultConfig())
	ret, err := db.ReadStatusHist(s.cfg.ConfigPath, n)
	if err != nil {
		return []*models.StatusFile{}, nil
	}
	return ret, nil
}

func defaultStatus(cfg *config.Config) *models.Status {
	return models.NewStatus(
		cfg,
		nil,
		scheduler.SchedulerStatus_None,
		int(models.PidNotRunning), nil, nil)
}