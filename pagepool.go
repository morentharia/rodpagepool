package rodpagepool

import (
	"context"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type PagePool struct {
	ch          chan proto.TargetTargetID
	b           *rod.Browser
	targedIDSet map[proto.TargetTargetID]bool
}

type PagePoolKey string

var Key PagePoolKey

func init() {
	Key = PagePoolKey("pagePool")
}

// NewPagePool instance
func NewPagePool(ctx context.Context, b *rod.Browser, limit int) (*PagePool, error) {
	pool := &PagePool{
		b:           b,
		ch:          make(chan proto.TargetTargetID, limit),
		targedIDSet: make(map[proto.TargetTargetID]bool),
	}
	for i := 0; i < limit; i++ {
		page, err := b.Page(proto.TargetCreateTarget{})
		if err != nil {
			return nil, errors.WithStack(err)
		}
		select {
		case pool.ch <- page.TargetID:
			pool.targedIDSet[page.TargetID] = true
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return pool, nil
}

func (pool PagePool) Get(ctx context.Context, timeout time.Duration) (*rod.Page, func(), error) {
	var once sync.Once
	select {
	case targedID := <-pool.ch:
		p, err := pool.b.PageFromTarget(targedID)
		//TODO: check for right error type "github.com/go-rod/rod/lib/cdp"
		if err != nil {
			delete(pool.targedIDSet, targedID)
			p, err = pool.b.Page(proto.TargetCreateTarget{})
			if err != nil {
				return nil, nil, errors.WithStack(err)
			}
			pool.targedIDSet[p.TargetID] = true
		}

		onceFunc := func() { pool.put(ctx, p) }
		go func() {
			<-time.After(timeout)
			once.Do(onceFunc) // NOTE goroutine leak
		}()

		p.Navigate("")
		p.WaitLoad()
		return p, func() { once.Do(onceFunc) }, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

}

func (pool PagePool) put(ctx context.Context, p *rod.Page) {
	select {
	case pool.ch <- p.TargetID:
	case <-ctx.Done():
		return
	}
}

func (pool PagePool) Cleanup(ctx context.Context) {
	for targetID := range pool.targedIDSet {
		p, err := pool.b.PageFromTarget(targetID)
		if err != nil {
			// logrus.WithError(err).Error("PageFromTarget")
			continue
		}
		err = p.Close()
		if err != nil {
			logrus.WithError(err).Error("page close")
		}
	}
}

func (pool PagePool) Fetch(ctx context.Context, rawurl string) (string, error) {
	page, put, err := pool.Get(ctx, 3*time.Second)
	if err != nil {
		logrus.WithError(err).Error("pool.Get")
		return "", errors.WithStack(err)
	}
	defer put()

	page.Navigate(rawurl)
	page.WaitLoad()
	time.Sleep(1 * time.Second)

	innerHTML := page.MustEval("document.documentElement.innerHTML").String()

	return innerHTML, nil
}
