// +build integration

// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package integration

import (
	"testing"
	"time"

	"github.com/m3db/m3db/integration/generate"
	"github.com/m3db/m3db/retention"
	"github.com/m3db/m3db/storage/namespace"
	"github.com/m3db/m3db/ts"
	xlog "github.com/m3db/m3x/log"
	xtime "github.com/m3db/m3x/time"

	"github.com/stretchr/testify/require"
)

// TODO:
// NB(rartoul): Delete this once we've tested V2 in prod
func TestPeersBootstrapSelectBest(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	testPeerBootstrapSelectBest(t, false)
}

func testPeerBootstrapSelectBest(t *testing.T, useV2 bool) {
	// Test setups
	log := xlog.SimpleLogger
	retentionOpts := retention.NewOptions().
		SetRetentionPeriod(6 * time.Hour).
		SetBlockSize(2 * time.Hour).
		SetBufferPast(10 * time.Minute).
		SetBufferFuture(2 * time.Minute)
	namesp, err := namespace.NewMetadata(testNamespaces[0], namespace.NewOptions().SetRetentionOptions(retentionOpts))
	require.NoError(t, err)
	opts := newTestOptions(t).
		SetNamespaces([]namespace.Metadata{namesp})

	setupOpts := []bootstrappableTestSetupOptions{
		{disablePeersBootstrapper: true},
		{disablePeersBootstrapper: true},
		{disablePeersBootstrapper: false, useV2PeerBootstrapper: useV2},
	}
	setups, closeFn := newDefaultBootstrappableTestSetups(t, opts, setupOpts)
	defer closeFn()

	// Write test data alternating missing data for left/right nodes
	now := setups[0].getNowFn()
	blockSize := retentionOpts.BlockSize()
	seriesMaps := generate.BlocksByStart([]generate.BlockConfig{
		{[]string{"foo", "bar"}, 180, now.Add(-blockSize)},
		{[]string{"foo", "baz"}, 90, now},
	})
	left := make(map[xtime.UnixNano]generate.SeriesBlock)
	right := make(map[xtime.UnixNano]generate.SeriesBlock)
	shouldMissData := false
	appendSeries := func(target map[xtime.UnixNano]generate.SeriesBlock, start time.Time, s generate.Series) {
		startNano := xtime.ToUnixNano(start)
		if shouldMissData {
			var dataWithMissing []ts.Datapoint
			for i := range s.Data {
				if i%2 != 0 {
					continue
				}
				dataWithMissing = append(dataWithMissing, s.Data[i])
			}
			target[startNano] = append(target[startNano], generate.Series{ID: s.ID, Data: dataWithMissing})
		} else {
			target[startNano] = append(target[startNano], s)
		}
		shouldMissData = !shouldMissData
	}
	for start, data := range seriesMaps {
		for _, series := range data {
			appendSeries(left, start.ToTime(), series)
			appendSeries(right, start.ToTime(), series)
		}
	}
	require.NoError(t, writeTestDataToDisk(namesp, setups[0], left))
	require.NoError(t, writeTestDataToDisk(namesp, setups[1], right))

	// Start the first two servers with filesystem bootstrappers
	setups[:2].parallel(func(s *testSetup) {
		require.NoError(t, s.startServer())
	})

	// Start the last server with peers and filesystem bootstrappers
	require.NoError(t, setups[2].startServer())
	log.Debug("servers are now up")

	// Stop the servers
	defer func() {
		setups.parallel(func(s *testSetup) {
			require.NoError(t, s.stopServer())
		})
		log.Debug("servers are now down")
	}()

	// Verify in-memory data match what we expect
	verifySeriesMaps(t, setups[0], namesp.ID(), left)
	verifySeriesMaps(t, setups[1], namesp.ID(), right)
	verifySeriesMaps(t, setups[2], namesp.ID(), seriesMaps)
}
