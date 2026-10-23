package mtclient

import (
	"encoding/binary"
	"fmt"

	"github.com/teamgram/proto/mtproto"
)

// Constructor returns the constructor of predicate (an mtproto.Predicate_*
// name) in layer, the one a client of that layer sends. The generated
// encoders write nothing for an object without a constructor, so requests
// and their nested objects set it explicitly.
func Constructor(predicate string, layer int32) mtproto.TLConstructor {
	id := mtproto.GetClazzID(predicate, int(layer))
	if id == 0 {
		panic(fmt.Sprintf("mtclient: no constructor of %s in layer %d", predicate, layer))
	}
	return mtproto.TLConstructor(id)
}

// Constructor returns the constructor of predicate in the client's layer.
func (c *Client) Constructor(predicate string) mtproto.TLConstructor {
	return Constructor(predicate, c.cfg.Layer)
}

// DecodeVector decodes a Vector of boxed objects, the result of methods such
// as users.getUsers (InvokeRaw).
func DecodeVector(raw []byte) ([]mtproto.TLObject, error) {
	if len(raw) < 8 || mtproto.TLConstructor(int32(binary.LittleEndian.Uint32(raw))) != mtproto.CRC32_vector {
		return nil, fmt.Errorf("mtclient: not a vector (%d bytes)", len(raw))
	}
	d := mtproto.NewDecodeBuf(raw[4:])
	n := d.Int()
	if n < 0 || int(n) > len(raw)/4 {
		return nil, fmt.Errorf("mtclient: vector of %d elements", n)
	}
	out := make([]mtproto.TLObject, 0, n)
	for i := int32(0); i < n; i++ {
		o := d.Object()
		if err := d.GetError(); err != nil {
			return nil, fmt.Errorf("mtclient: vector element %d: %w", i, err)
		}
		out = append(out, o)
	}
	return out, nil
}
