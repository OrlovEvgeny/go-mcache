package store

import "github.com/RoaringBitmap/roaring/v2"

// expiryBucket delays Roaring's container allocations for small ID sets.
// The shard lock protects both representations. IDs are unique on insertion.
type expiryBucket struct {
	small  [16]uint32
	count  uint8
	bitmap *roaring.Bitmap
}

func (b *expiryBucket) Add(id uint32) {
	if b.bitmap != nil {
		b.bitmap.Add(id)
		return
	}
	if int(b.count) < len(b.small) {
		b.small[b.count] = id
		b.count++
		return
	}
	b.promote(id)
}

func (b *expiryBucket) promote(id uint32) {
	b.bitmap = roaring.New()
	b.bitmap.AddMany(b.small[:b.count])
	b.bitmap.Add(id)
	b.count = 0
}

func (b *expiryBucket) Remove(id uint32) {
	if b.bitmap != nil {
		b.bitmap.Remove(id)
		return
	}
	for i, x := range b.small[:b.count] {
		if x == id {
			b.count--
			b.small[i] = b.small[b.count]
			return
		}
	}
}

func (b *expiryBucket) IsEmpty() bool {
	if b.bitmap != nil {
		return b.bitmap.IsEmpty()
	}
	return b.count == 0
}

type expiryIterator struct {
	small  []uint32
	bitmap roaring.IntIterable
}

func (b *expiryBucket) Iterator() expiryIterator {
	if b.bitmap != nil {
		return expiryIterator{bitmap: b.bitmap.Iterator()}
	}
	return expiryIterator{small: b.small[:b.count]}
}
func (it *expiryIterator) HasNext() bool {
	if it.bitmap != nil {
		return it.bitmap.HasNext()
	}
	return len(it.small) > 0
}
func (it *expiryIterator) Next() uint32 {
	if it.bitmap != nil {
		return it.bitmap.Next()
	}
	id := it.small[0]
	it.small = it.small[1:]
	return id
}
