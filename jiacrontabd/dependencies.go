package jiacrontabd

import (
	"bytes"
	"context"
	"fmt"
	"jiacrontab/pkg/log"
	"jiacrontab/pkg/proto"
	"path/filepath"
	"time"
)

type depEntry struct {
	jobID      int    // 定时任务id
	processID  int    // 当前依赖的父级任务（可能存在多个并发的task
	id         string // 依赖id
	from       string //
	commands   []string
	dest       string
	done       bool
	timeout    int64
	err        error
	name       string
	logContent []byte
}

func newDependencies(crond *Jiacrontabd) *dependencies {
	return &dependencies{
		crond: crond,
		dep:   make(chan *depEntry, 100),
	}
}

type dependencies struct {
	crond *Jiacrontabd
	dep   chan *depEntry
}

func (d *dependencies) add(t *depEntry) {
	d.dep <- t
}

func (d *dependencies) run() {
	go func() {
		for {
			select {
			case t := <-d.dep:
				go func(t *depEntry) {}(t)
			}
		}
	}()
}

func (d *dependencies) exec(task *depEntry) {

	var (
		reply      bool
		logContent []byte
		errMsg     string
	)

	if task.timeout == 0 {
		// 默认超时10分钟
		task.timeout = 600
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.timeout)*time.Second)

	startTime := time.Now()
	start := startTime.UnixNano()
	cmdList := [][]string{task.commands}
	logPath := filepath.Join(cfg.LogPath, "depend_job")

	err := wrapExecScript(ctx, fmt.Sprintf("%d-%s.log", task.jobID, task.id), cmdList, logPath, &logContent)
	cancel()
	costTime := time.Now().UnixNano() - start

	log.Infof("exec %s %s cost %.4fs %v", task.name, task.commands, float64(costTime)/1000000000, err)

	task.logContent = bytes.TrimRight(logContent, "\x00")
	task.done = true
	task.err = err

	if err != nil {
		errMsg = err.Error()
	} else {
		errMsg = ""
	}

	task.dest, task.from = task.from, task.dest

	if !d.crond.filterDepend(task) {
		err = rpcCall("Logic.DependDone", proto.DependsTask{
			ID:         task.id,
			Name:       task.name,
			Dest:       task.dest,
			From:       task.from,
			ProcessID:  task.processID,
			JobEntryID: task.jobID,
			Commands:   task.commands,
			LogContent: task.logContent,
			Err:        errMsg,
			Timeout:    task.timeout,
		}, &reply)

		if err != nil {
			log.Error("Logic.DependDone error:", err, "server addr:", cfg.AdminAddr)
		}

		if !reply {
			log.Errorf("task %s %s %s call Logic.DependDone failed! err:%v", task.name, task.commands, err)
		}
	}
}
