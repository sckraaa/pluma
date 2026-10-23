package idgen_client

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/idgen/idgen"
)

// fakeIdgen answers nextId and nextIds with fixed values or an error.
type fakeIdgen struct {
	IdgenClient // the other methods are not called
	id          int64
	ids         []int64
	err         error
}

func (f *fakeIdgen) IdgenNextId(context.Context, *idgen.TLIdgenNextId) (*mtproto.Int64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &mtproto.Int64{V: f.id}, nil
}

func (f *fakeIdgen) IdgenNextIds(context.Context, *idgen.TLIdgenNextIds) (*idgen.Vector_Long, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &idgen.Vector_Long{Datas: f.ids}, nil
}

// An idgen failure reaches the caller as an error, never as id 0
// (audit finding #2).
func TestNextIdErrors(t *testing.T) {
	ctx := context.Background()
	down := errors.New("idgen unavailable")

	for _, c := range []struct {
		name string
		f    *fakeIdgen
		ok   bool
	}{
		{"id", &fakeIdgen{id: 42}, true},
		{"rpc error", &fakeIdgen{err: down}, false},
		{"zero", &fakeIdgen{id: 0}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := &IDGenClient2{cli: c.f}
			id, err := m.NextId(ctx)
			if c.ok {
				if err != nil || id != 42 {
					t.Fatalf("NextId = %d, %v", id, err)
				}
				return
			}
			if !errors.Is(err, mtproto.ErrInternalServerError) || id != 0 {
				t.Fatalf("NextId = %d, %v; want INTERNAL_SERVER_ERROR", id, err)
			}
		})
	}

	for _, c := range []struct {
		name string
		f    *fakeIdgen
		ok   bool
	}{
		{"ids", &fakeIdgen{ids: []int64{1, 2, 3}}, true},
		{"rpc error", &fakeIdgen{err: down}, false},
		{"too few", &fakeIdgen{ids: []int64{1, 2}}, false},
		{"a zero", &fakeIdgen{ids: []int64{1, 0, 3}}, false},
	} {
		t.Run("NextIds "+c.name, func(t *testing.T) {
			m := &IDGenClient2{cli: c.f}
			ids, err := m.NextIds(ctx, 3)
			if c.ok {
				if err != nil || len(ids) != 3 {
					t.Fatalf("NextIds = %v, %v", ids, err)
				}
				return
			}
			if !errors.Is(err, mtproto.ErrInternalServerError) || ids != nil {
				t.Fatalf("NextIds = %v, %v; want INTERNAL_SERVER_ERROR", ids, err)
			}
		})
	}
}
