package collector

import (
	"sync"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
)

// Collector is interface for anything that can provide information about
// managers.
type Collector interface {
	Managers() []*api.ManagerInfo
}

// ManagerConn is *grpc.ClientConn plus string representation of Address.
type ManagerConn struct {
	Conn *grpc.ClientConn
	Addr string
}

// ConnectionsList provides connections for managers.
type ConnectionsList interface {
	Connections() []*ManagerConn
}

// ClusterCollector collects information from multiple managers by grpc.
type ClusterCollector struct {
	conns  ConnectionsList
	config *Config
	info   []*api.ManagerInfo
	mu     sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

// Config is configuration of ClusterCollector.
type Config struct {
	Tick    time.Duration
	Timeout time.Duration
}

// DefaultConfig returns default config for ClusterCollector
func DefaultConfig() *Config {
	return &Config{
		Tick:    5 * time.Second,
		Timeout: 3 * time.Second,
	}
}

// New returns new ClusterCollector with ConnectionsList and Config.
// ctx can be used to stop ClusterCollector.
func New(ctx context.Context, conns ConnectionsList, c *Config) *ClusterCollector {
	ctx, cancel := context.WithCancel(ctx)
	return &ClusterCollector{
		conns:  conns,
		config: c,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Run starts collecting information. It blocks until get first responses.
// Then it iterates over ConnectionsList.Connections() each Config.Tick.
func (b *ClusterCollector) Run() error {
	b.updateInfo()
	for {
		select {
		case <-time.Tick(b.config.Tick):
			b.updateInfo()
		case <-b.ctx.Done():
			logrus.Debugf("collector: stop collector")
			return nil
		}
	}
}

// Stop stops ClusterCollector. It just cancels underlying context.Context.
func (b *ClusterCollector) Stop() {
	b.cancel()
}

func (b *ClusterCollector) updateInfo() {
	conns := b.conns.Connections()
	miChan := make(chan *api.ManagerInfo, len(conns))
	var wg sync.WaitGroup
	ctx, _ := context.WithTimeout(b.ctx, b.config.Timeout)
	for _, conn := range conns {
		wg.Add(1)
		go func(conn *ManagerConn) {
			defer wg.Done()
			resp, err := api.NewManagerClient(conn.Conn).NodeCount(ctx, nil)
			if err != nil {
				logrus.Errorf("collector: failed to receive node count from %s: %v", conn.Addr, err)
				return
			}
			miChan <- &api.ManagerInfo{Addr: conn.Addr, NodeCount: resp.Count}
		}(conn)
	}
	wg.Wait()
	close(miChan)
	var res []*api.ManagerInfo
	for mi := range miChan {
		res = append(res, mi)
	}
	b.mu.Lock()
	b.info = res
	b.mu.Unlock()
}

// Managers returns recent information about managers.
func (b *ClusterCollector) Managers() []*api.ManagerInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.info
}
