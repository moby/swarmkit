package etcd

import (
	"context"
	"crypto/tls"
	"path"
	"strconv"
	"strings"
	"time"

	etcd "github.com/coreos/etcd/clientv3"
	etcdsync "github.com/coreos/etcd/clientv3/concurrency"
	log "github.com/sirupsen/logrus"
	"github.com/akutz/gosync"

	csictx "github.com/rexray/gocsi/context"
	mwtypes "github.com/rexray/gocsi/middleware/serialvolume/types"
)

// New returns a new etcd volume lock provider.
func New(
	ctx context.Context,
	domain string,
	ttl time.Duration,
	config *etcd.Config) (mwtypes.VolumeLockerProvider, error) {

	fields := map[string]interface{}{}

	if domain == "" {
		domain = csictx.Getenv(ctx, EnvVarDomain)
	}
	domain = path.Join("/", domain)
	fields["serialvol.etcd.domain"] = domain

	if ttl == 0 {
		ttl, _ = time.ParseDuration(csictx.Getenv(ctx, EnvVarTTL))
		if ttl > 0 {
			fields["serialvol.etcd.ttl"] = ttl
		}
	}

	if config == nil {
		cfg, err := initConfig(ctx, fields)
		if err != nil {
			return nil, err
		}
		config = &cfg
	}

	log.WithFields(fields).Info("creating serial vol etcd lock provider")

	client, err := etcd.New(*config)
	if err != nil {
		return nil, err
	}

	return &provider{
		client: client,
		domain: domain,
		ttl:    int(ttl.Seconds()),
	}, nil
}

func initConfig(
	ctx context.Context,
	fields map[string]interface{}) (etcd.Config, error) {

	config := etcd.Config{}

	if v := csictx.Getenv(ctx, EnvVarEndpoints); v != "" {
		config.Endpoints = strings.Split(v, ",")
		fields["serialvol.etcd.Endpoints"] = v
	}

	if v := csictx.Getenv(ctx, EnvVarAutoSyncInterval); v != "" {
		v, err := time.ParseDuration(v)
		if err != nil {
			return config, err
		}
		config.AutoSyncInterval = v
		fields["serialvol.etcd.AutoSyncInterval"] = v
	}

	if v := csictx.Getenv(ctx, EnvVarDialKeepAliveTime); v != "" {
		v, err := time.ParseDuration(v)
		if err != nil {
			return config, err
		}
		config.DialKeepAliveTime = v
		fields["serialvol.etcd.DialKeepAliveTime"] = v
	}

	if v := csictx.Getenv(ctx, EnvVarDialKeepAliveTimeout); v != "" {
		v, err := time.ParseDuration(v)
		if err != nil {
			return config, err
		}
		config.DialKeepAliveTimeout = v
		fields["serialvol.etcd.DialKeepAliveTimeout"] = v
	}

	if v := csictx.Getenv(ctx, EnvVarDialTimeout); v != "" {
		v, err := time.ParseDuration(v)
		if err != nil {
			return config, err
		}
		config.DialTimeout = v
		fields["serialvol.etcd.DialTimeout"] = v
	}

	if v := csictx.Getenv(ctx, EnvVarMaxCallRecvMsgSz); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			return config, err
		}
		config.MaxCallRecvMsgSize = i
		fields["serialvol.etcd.MaxCallRecvMsgSize"] = i
	}

	if v := csictx.Getenv(ctx, EnvVarMaxCallSendMsgSz); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			return config, err
		}
		config.MaxCallSendMsgSize = i
		fields["serialvol.etcd.MaxCallSendMsgSize"] = i
	}

	if v := csictx.Getenv(ctx, EnvVarUsername); v != "" {
		config.Username = v
		fields["serialvol.etcd.Username"] = v
	}
	if v := csictx.Getenv(ctx, EnvVarPassword); v != "" {
		config.Password = v
		fields["serialvol.etcd.Password"] = "********"
	}

	if v, ok := csictx.LookupEnv(ctx, EnvVarRejectOldCluster); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return config, err
		}
		config.RejectOldCluster = b
		fields["serialvol.etcd.RejectOldCluster"] = b
	}

	if v, ok := csictx.LookupEnv(ctx, EnvVarTLS); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return config, err
		}
		if b {
			config.TLS = &tls.Config{}
			fields["serialvol.etcd.tls"] = b
			if v, ok := csictx.LookupEnv(ctx, EnvVarTLSInsecure); ok {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return config, err
				}
				config.TLS.InsecureSkipVerify = b
				fields["serialvol.etcd.tls.insecure"] = b
			}
		}
	}

	return config, nil
}

type provider struct {
	client *etcd.Client
	domain string
	ttl    int
}

func (p *provider) Close() error {
	return p.client.Close()
}

func (p *provider) GetLockWithID(
	ctx context.Context, id string) (gosync.TryLocker, error) {

	return p.getLock(ctx, path.Join(p.domain, "volumesByID", id))
}

func (p *provider) GetLockWithName(
	ctx context.Context, name string) (gosync.TryLocker, error) {

	return p.getLock(ctx, path.Join(p.domain, "volumesByName", name))
}

func (p *provider) getLock(
	ctx context.Context, pfx string) (gosync.TryLocker, error) {

	log.Debugf("EtcdVolumeLockProvider: getLock: pfx=%v", pfx)

	opts := []etcdsync.SessionOption{etcdsync.WithContext(ctx)}
	if p.ttl > 0 {
		opts = append(opts, etcdsync.WithTTL(p.ttl))
	}

	sess, err := etcdsync.NewSession(p.client, opts...)
	if err != nil {
		return nil, err
	}
	return &TryMutex{
		ctx: ctx, sess: sess, mtx: etcdsync.NewMutex(sess, pfx)}, nil
}

// TryMutex is a mutual exclusion lock backed by etcd that implements the
// TryLocker interface.
// The zero value for a TryMutex is an unlocked mutex.
//
// A TryMutex may be copied after first use.
type TryMutex struct {
	ctx  context.Context
	sess *etcdsync.Session
	mtx  *etcdsync.Mutex

	// LockCtx, when non-nil, is the context used with Lock.
	LockCtx context.Context

	// UnlockCtx, when non-nil, is the context used with Unlock.
	UnlockCtx context.Context

	// TryLockCtx, when non-nil, is the context used with TryLock.
	TryLockCtx context.Context
}

// Lock locks m. If the lock is already in use, the calling goroutine blocks
// until the mutex is available.
func (m *TryMutex) Lock() {
	//log.Debug("TryMutex: lock")
	ctx := m.LockCtx
	if ctx == nil {
		ctx = m.ctx
	}
	if err := m.mtx.Lock(ctx); err != nil {
		log.Debugf("TryMutex: lock err: %v", err)
		if err != context.Canceled && err != context.DeadlineExceeded {
			log.Panicf("TryMutex: lock panic: %v", err)
		}
	}
}

// Unlock unlocks m. It is a run-time error if m is not locked on entry to
// Unlock.
//
// A locked TryMutex is not associated with a particular goroutine. It is
// allowed for one goroutine to lock a Mutex and then arrange for another
// goroutine to unlock it.
func (m *TryMutex) Unlock() {
	//log.Debug("TryMutex: unlock")
	ctx := m.UnlockCtx
	if ctx == nil {
		ctx = m.ctx
	}
	if err := m.mtx.Unlock(ctx); err != nil {
		log.Debugf("TryMutex: unlock err: %v", err)
		if err != context.Canceled && err != context.DeadlineExceeded {
			log.Panicf("TryMutex: unlock panic: %v", err)
		}
	}
}

// Close closes and cleans up the underlying concurrency session.
func (m *TryMutex) Close() error {
	//log.Debug("TryMutex: close")
	if err := m.sess.Close(); err != nil {
		log.Errorf("TryMutex: close err: %v", err)
		return err
	}
	return nil
}

// TryLock attempts to lock m. If no lock can be obtained in the specified
// duration then a false value is returned.
func (m *TryMutex) TryLock(timeout time.Duration) bool {

	ctx := m.TryLockCtx
	if ctx == nil {
		ctx = m.ctx
	}

	// Create a timeout context only if the timeout is greater than zero.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if err := m.mtx.Lock(ctx); err != nil {
		log.Debugf("TryMutex: TryLock err: %v", err)
		if err != context.Canceled && err != context.DeadlineExceeded {
			log.Panicf("TryMutex: TryLock panic: %v", err)
		}
		return false
	}
	return true
}
