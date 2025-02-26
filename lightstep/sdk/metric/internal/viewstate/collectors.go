// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package viewstate // import "github.com/lightstep/otel-launcher-go/lightstep/sdk/metric/internal/viewstate"

import (
	"sync/atomic"

	"github.com/lightstep/otel-launcher-go/lightstep/sdk/metric/aggregator"
	"github.com/lightstep/otel-launcher-go/lightstep/sdk/metric/aggregator/aggregation"
	"github.com/lightstep/otel-launcher-go/lightstep/sdk/metric/data"
	"github.com/lightstep/otel-launcher-go/lightstep/sdk/metric/internal/pipeline"
	"github.com/lightstep/otel-launcher-go/lightstep/sdk/metric/number"
	"go.opentelemetry.io/otel/attribute"
)

// statefulSyncInstrument is a synchronous instrument that maintains cumulative state.
type statefulSyncInstrument[N number.Any, Storage any, Methods aggregator.Methods[N, Storage], Samp SampleFilter] struct {
	compiledSyncBase[N, Storage, Methods, Samp]
}

// Collect for synchronous cumulative temporality.
func (p *statefulSyncInstrument[N, Storage, Methods, Samp]) Collect(seq data.Sequence, output *[]data.Instrument) {
	p.instLock.Lock()
	defer p.instLock.Unlock()

	ioutput := p.appendInstrument(output)

	for set, entry := range p.data {
		p.appendPoint(ioutput, set, &entry.storage, aggregation.CumulativeTemporality, seq.Start, seq.Now, false)
	}
}

// lowmemorySyncInstrument is a synchronous instrument that maintains no state.
type lowmemorySyncInstrument[N number.Any, Storage any, Methods aggregator.Methods[N, Storage], Samp SampleFilter] struct {
	compiledSyncBase[N, Storage, Methods, Samp]
}

// Collect for synchronous delta temporality.
func (p *lowmemorySyncInstrument[N, Storage, Methods, Samp]) Collect(seq data.Sequence, output *[]data.Instrument) {
	var methods Methods

	p.instLock.Lock()
	defer p.instLock.Unlock()

	ioutput := p.appendInstrument(output)

	for set, entry := range p.data {
		// capture the number of references before the Move() call
		// below.  we're holding the lock that prevents new refs, so
		// the value before Move() indicates when it's safe to remove
		// this entry from the map.
		numRefs := atomic.LoadInt64(&entry.auxiliary)

		p.appendPoint(ioutput, set, &entry.storage, aggregation.DeltaTemporality, seq.Last, seq.Now, true)

		// By passing reset=true above, the aggregator data in
		// entry.storage has been moved into the last index of
		// ioutput.Points.
		ptsArr := ioutput.Points
		point := &ptsArr[len(ptsArr)-1]

		cpy, _ := methods.ToStorage(point.Aggregation)

		if methods.HasChange(cpy) {
			continue
		}

		// We allowed the array to grow before the above
		// test speculatively, since when it succeeds
		// we are able to re-use the underlying
		// aggregator.  Here, undo the new element.
		ioutput.Points = ptsArr[0 : len(ptsArr)-1 : cap(ptsArr)]

		// If there are no more accumulator references to the
		// entry, remove from the map.
		if numRefs == 0 {
			delete(p.data, set)
		}
	}
}

// lowmemoryAsyncInstrument is an asynchronous instrument that keeps
// maintains no state.
type lowmemoryAsyncInstrument[N number.Any, Storage any, Methods aggregator.Methods[N, Storage]] struct {
	compiledAsyncBase[N, Storage, Methods]
}

// Collect for asynchronous cumulative temporality.
func (p *lowmemoryAsyncInstrument[N, Storage, Methods]) Collect(seq data.Sequence, output *[]data.Instrument) {
	p.instLock.Lock()
	defer p.instLock.Unlock()

	ioutput := p.appendInstrument(output)

	for set, entry := range p.data {
		p.appendPoint(ioutput, set, &entry.storage, aggregation.CumulativeTemporality, seq.Start, seq.Now, false)
	}

	// Reset the entire map.
	p.data = map[attribute.Set]*storageHolder[Storage, notUsed]{}
}

// statefulAsyncInstrument is an instrument that keeps asynchronous instrument state
// in order to perform cumulative to delta translation.
type statefulAsyncInstrument[N number.Any, Storage any, Methods aggregator.Methods[N, Storage]] struct {
	compiledAsyncBase[N, Storage, Methods]
	prior map[attribute.Set]*storageHolder[Storage, notUsed]
}

// Size (special case) reports the size of the prior map, since
// data is emptied on Collect().
func (p *statefulAsyncInstrument[N, Storage, Methods]) InMemorySize() int {
	p.instLock.Lock()
	defer p.instLock.Unlock()
	return len(p.prior)
}

// Collect for asynchronous delta temporality.  Note this code path is
// not used for Gauge instruments.
func (p *statefulAsyncInstrument[N, Storage, Methods]) Collect(seq data.Sequence, output *[]data.Instrument) {
	var methods Methods

	p.instLock.Lock()
	defer p.instLock.Unlock()

	ioutput := p.appendInstrument(output)

	var ofe *storageHolder[Storage, notUsed]
	for set, entry := range p.data {
		// Compute the difference.
		pval, has := p.prior[set]

		if set == pipeline.OverflowAttributeSet {
			ofe = entry
		}
		if has {
			// This does `*pval := *storage - *pval`
			methods.SubtractSwap(&pval.storage, &entry.storage)

			// Skip the series if it has not changed.
			if !methods.HasChange(&pval.storage) {
				continue
			}
			entry = pval
		}
		p.appendPoint(ioutput, set, &entry.storage, aggregation.DeltaTemporality, seq.Last, seq.Now, false)
	}
	// TODO: Values that are contained in prior but not in data
	// should be copied so they are not forgotten and do not
	// output spurious counts in the future when they reappear.
	// This is only an issue for asynchronous instruments with
	// delta temporality.

	// Copy the current to the prior and reset.
	p.prior = p.data
	p.data = map[attribute.Set]*storageHolder[Storage, notUsed]{}

	// Note: the overflow attribute set is synthesized from a
	// number of inputs which are presumed cumulative.  To maintain this
	// illusion, copy its current cumulative value into the next data set.
	if ofe != nil {
		cpy := &storageHolder[Storage, notUsed]{}
		methods.Copy(&ofe.storage, &cpy.storage)

		p.data[pipeline.OverflowAttributeSet] = cpy
	}
}
