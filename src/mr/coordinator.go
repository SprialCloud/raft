package mr

import (
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

//管理任务状态
//处理worker的RPC请求
//检测是否超时
//协调map和reduce任务的分配

type Task struct {
	FileName  string
	Status    string
	StartTime time.Time
	TaskID    int
}

type Coordinator struct {
	mapTasks    []Task
	reduceTasks []Task
	nReduce     int
	mapFinished bool
	allFinished bool
	files       []string
	nextTaskID  int
	mu          sync.Mutex
}

// Your code here -- RPC handlers for the worker to call.

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

func (c *Coordinator) GetTask(args *GetTaskArgs, reply *GetTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	//检测超时任务
	c.checkTimeout()

	//如果所有任务都已完成，通知worker退出
	if c.allFinished {
		reply.TaskType = ExitTask
		return nil
	}

	//如果map任务有剩余，分配map任务
	if !c.mapFinished {
		for i, task := range c.mapTasks {
			if task.Status == Idle {
				reply.TaskType = MapTask
				reply.FileName = task.FileName
				reply.TaskID = task.TaskID
				reply.NReduce = c.nReduce

				//更新任务状态
				c.mapTasks[i].Status = InProgress
				c.mapTasks[i].StartTime = time.Now()
				return nil
			}
		}
		reply.TaskType = WaitTask
		return nil
	}
	//如果所有map任务都已完成，分配reduce任务
	for i, task := range c.reduceTasks {
		if task.Status == Idle {
			reply.TaskType = ReduceTask
			reply.TaskID = task.TaskID
			reply.ReduceTaskNum = i
			reply.MapTaskNum = len(c.mapTasks)
			//更新任务状态
			c.reduceTasks[i].Status = InProgress
			c.reduceTasks[i].StartTime = time.Now()
			return nil
		}
	}
	reply.TaskType = WaitTask
	return nil
}

// 检查超时任务
func (c *Coordinator) checkTimeout() {
	timeout := 10 * time.Second

	now := time.Now()
	//检查map任务超时
	if !c.mapFinished {
		allCompleted := true
		for i, task := range c.mapTasks {
			if task.Status == InProgress && now.Sub(task.StartTime) > timeout {
				//超时，重新分配任务
				c.mapTasks[i].Status = Idle
			}
			if task.Status != Completed {
				allCompleted = false
			}
		}
		c.mapFinished = allCompleted
	}
	//检查reduce任务超时
	allCompleted := true
	for i, task := range c.reduceTasks {
		if task.Status == InProgress && now.Sub(task.StartTime) > timeout {
			//超时，重新分配任务
			c.reduceTasks[i].Status = Idle
		}
		if task.Status != Completed {
			allCompleted = false
		}
	}
	c.allFinished = allCompleted
}

func (c *Coordinator) ReportTask(args *ReportTaskArgs, reply *ReportTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if args.TaskType == MapTask {
		for i, task := range c.mapTasks {
			if task.TaskID == args.TaskID && task.Status == InProgress {
				c.mapTasks[i].Status = Completed

				//检查是否所有map任务都已完成
				allCompleted := true
				for _, task := range c.mapTasks {
					if task.Status != Completed {
						allCompleted = false
						break
					}
				}
				c.mapFinished = allCompleted
				reply.OK = true
				return nil
			}
		}
	} else if args.TaskType == ReduceTask {
		for i, task := range c.reduceTasks {
			if task.TaskID == args.TaskID && task.Status == InProgress {
				c.reduceTasks[i].Status = Completed

				//检查是否所有reduce任务都已完成
				allCompleted := true
				for _, task := range c.reduceTasks {
					if task.Status != Completed {
						allCompleted = false
						break
					}
				}
				c.allFinished = allCompleted
				reply.OK = true
				return nil
			}
		}
	}
	reply.OK = false
	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	ret := false

	if c.allFinished {
		ret = true
	}

	return ret
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		files:       files,
		nReduce:     nReduce,
		nextTaskID:  0,
		mapFinished: false,
		allFinished: false,
		mapTasks:    make([]Task, len(files)),
		reduceTasks: make([]Task, nReduce),
	}

	//初始化map任务
	for i, file := range c.files {
		c.mapTasks[i] = Task{
			FileName: file,
			Status:   Idle,
			TaskID:   i,
		}
	}
	//初始化reduce任务
	for i := 0; i < nReduce; i++ {
		c.reduceTasks[i] = Task{
			Status: Idle,
			TaskID: i,
		}
	}
	c.server()
	return &c
}
