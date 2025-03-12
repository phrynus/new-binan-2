package main

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 任务结构（函数参数前置）
type Task struct {
	exec     func()        // 任务函数
	priority int           // 执行优先级
	timeout  time.Duration // 超时时间
}

// 优先队列实现
type PriorityQueue []*Task

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].priority > pq[j].priority // 值越大优先级越高
}
func (pq PriorityQueue) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }

func (pq *PriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*Task))
	heap.Fix(pq, len(*pq)-1) // 插入后立即排序
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	task := old[n-1]
	*pq = old[0 : n-1]
	heap.Fix(pq, 0) // 弹出后重新排序
	return task
}

// 线程池结构
type ThreadPool struct {
	queue       PriorityQueue
	maxWorkers  int
	rateLimit   int
	rateWindow  time.Duration
	activeTasks chan struct{}
	rateTokens  chan time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	queueLock   sync.Mutex
	wg          sync.WaitGroup

	successCount atomic.Int32
	failureCount atomic.Int32
}

// 创建线程池
func NewThreadPool(workers, rateLimit int, rateWindow time.Duration) *ThreadPool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &ThreadPool{
		queue:       make(PriorityQueue, 0),
		maxWorkers:  workers,
		rateLimit:   rateLimit,
		rateWindow:  rateWindow,
		activeTasks: make(chan struct{}, workers),
		rateTokens:  make(chan time.Time, rateLimit),
		ctx:         ctx,
		cancel:      cancel,
	}

	heap.Init(&p.queue)
	go p.tokenGenerator()
	return p
}

// 令牌生成器
func (p *ThreadPool) tokenGenerator() {
	ticker := time.NewTicker(p.rateWindow / time.Duration(p.rateLimit))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			select {
			case p.rateTokens <- time.Now():
			default:
			}
		case <-p.ctx.Done():
			return
		}
	}
}

// 提交任务（函数参数前置）
func (p *ThreadPool) Submit(fn func(), opts ...func(*Task)) {
	task := &Task{
		exec:     fn,
		priority: 0,               // 默认优先级
		timeout:  5 * time.Second, // 默认超时
	}

	// 应用可选参数
	for _, opt := range opts {
		opt(task)
	}

	p.queueLock.Lock()
	heap.Push(&p.queue, task)
	p.queueLock.Unlock()

	go p.tryExecute()
}

// 尝试执行任务
func (p *ThreadPool) tryExecute() {
	select {
	case <-p.ctx.Done():
		p.failureCount.Add(1)
		return
	default:
	}

	// 双重限制检查
	select {
	case ts := <-p.rateTokens:
		if time.Since(ts) > p.rateWindow {
			p.failureCount.Add(1)
			return
		}
	default:
		p.failureCount.Add(1)
		return
	}

	select {
	case p.activeTasks <- struct{}{}:
		defer func() { <-p.activeTasks }()
	default:
		p.failureCount.Add(1)
		return
	}

	p.executeTask()
}

// 执行任务
func (p *ThreadPool) executeTask() {
	p.queueLock.Lock()
	if len(p.queue) == 0 {
		p.queueLock.Unlock()
		return
	}
	task := heap.Pop(&p.queue).(*Task)
	p.queueLock.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.handlePanic()

		ctx, cancel := context.WithTimeout(p.ctx, task.timeout)
		defer cancel()

		done := make(chan struct{})
		go func() {
			task.exec()
			close(done)
		}()

		select {
		case <-done:
			p.successCount.Add(1)
		case <-ctx.Done():
			p.failureCount.Add(1)
		}
	}()
}

// 异常处理
func (p *ThreadPool) handlePanic() {
	if r := recover(); r != nil {
		p.failureCount.Add(1)
		fmt.Printf("任务执行异常: %v\n", r)
	}
}

// 停止线程池
func (p *ThreadPool) Stop() {
	p.cancel()
	p.wg.Wait()

	p.queueLock.Lock()
	p.failureCount.Add(int32(len(p.queue)))
	p.queue = make(PriorityQueue, 0)
	p.queueLock.Unlock()

	fmt.Printf("执行结果: 成功=%d 失败=%d\n",
		p.successCount.Load(),
		p.failureCount.Load())
}

// 可选参数设置
func WithPriority(priority int) func(*Task) {
	return func(t *Task) {
		t.priority = priority
	}
}

func WithTimeout(timeout time.Duration) func(*Task) {
	return func(t *Task) {
		t.timeout = timeout
	}
}

func main() {
	// 创建线程池：3个worker，每秒2个请求
	pool := NewThreadPool(3, 2, time.Second)
	fmt.Println("线程池创建成功\n")
	// 提交测试任务（优先级倒序）
	for i := 10; i > 0; i-- {
		func(priority int) {
			pool.Submit(
				func() {
					time.Sleep(500 * time.Millisecond)
					fmt.Printf("任务%d 执行时间: %s\n",
						priority, time.Now().Format("15:04:05.000"))
				},
				WithPriority(priority),
				WithTimeout(3*time.Second),
			)
		}(i)
	}

	time.Sleep(30 * time.Second)
	pool.Stop()
}
