package tlb

import (
	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/noc"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/akita/v3/tracing"
)

// RingNoC represents a bidirectional ring topology for L1-TLBs in a Shader Engine
type RingNoC struct {
	*sim.ComponentBase
	TLBs    []*L1TLB // Array of 16 L1-TLBs in the SE
	NumTLBs int      // Number of TLBs (16 per SE)
	SEID    int      // Shader Engine ID
	engine  sim.Engine
	conn    *noc.Connection
}

// L1TLB extends TLB with ring NoC-specific fields
type L1TLB struct {
	*TLB
	ID             int            // Unique TLB ID within the SE (0 to 15)
	PrefetchBuffer []vm.Page      // 24-entry prefetch buffer
	ProbeQueue     []ProbeRequest // 16-entry queue for probe requests
	Ring           *RingNoC       // Reference to the ring NoC
}

// ProbeRequest represents a TLB probe request
type ProbeRequest struct {
	sim.MsgMeta
	VirtualAddr uint64 // Virtual address to probe
	TTL         int    // Time-To-Live (4 for counterclockwise, 15 for clockwise)
	Direction   string // "clockwise" or "counterclockwise"
	SourceTLB   int    // TLB ID of the requesting TLB
	SEID        int    // Shader Engine ID
}

// ProbeResponse represents a response to a probe request
type ProbeResponse struct {
	sim.MsgMeta
	VirtualAddr uint64   // Virtual address probed
	Page        *vm.Page // Translation result (nil if not found)
	SourceTLB   int      // TLB ID of the requesting TLB
	SEID        int      // Shader Engine ID
}

// Meta returns the meta data of the probe request
func (r *ProbeRequest) Meta() *sim.MsgMeta {
	return &r.MsgMeta
}

// Meta returns the meta data of the probe response
func (r *ProbeResponse) Meta() *sim.MsgMeta {
	return &r.MsgMeta
}

// NewRingNoC creates a new bidirectional ring for an SE
func NewRingNoC(name string, engine sim.Engine, seID int, builder Builder) *RingNoC {
	ring := &RingNoC{
		ComponentBase: sim.NewComponentBase(name),
		NumTLBs:       16, // 16 TLBs per SE, per paper
		TLBs:          make([]*L1TLB, 16),
		SEID:          seID,
		engine:        engine,
	}
	ring.conn = noc.NewConnection(name+".Connection", engine, 1)
	ring.conn.PlugIn(ring, 16) // Support 16 TLBs

	// Initialize each L1-TLB
	for i := 0; i < ring.NumTLBs; i++ {
		tlb := builder.Build(name + ".L1VTLB" + string(rune(i)))
		ring.TLBs[i] = &L1TLB{
			TLB:            tlb,
			ID:             i,
			PrefetchBuffer: make([]vm.Page, 24),         // 24 entries for prefetch buffer
			ProbeQueue:     make([]ProbeRequest, 0, 16), // 16-entry probe queue
			Ring:           ring,
		}
	}

	return ring
}

// InitializeRingNoCs initializes rings for all SEs (assuming 4 SEs)
func InitializeRingNoCs(numSEs int, engine sim.Engine, builder Builder) []*RingNoC {
	rings := make([]*RingNoC, numSEs)
	for i := 0; i < numSEs; i++ {
		rings[i] = NewRingNoC("RingNoC_SE"+string(rune(i)), engine, i, builder)
	}
	return rings
}

// GetNextTLB returns the next TLB in the specified direction
func (ring *RingNoC) GetNextTLB(currentTLBID int, direction string) *L1TLB {
	if direction == "clockwise" {
		nextID := (currentTLBID + 1) % ring.NumTLBs
		return ring.TLBs[nextID]
	} else if direction == "counterclockwise" {
		nextID := (currentTLBID - 1 + ring.NumTLBs) % ring.NumTLBs
		return ring.TLBs[nextID]
	}
	return nil
}

// SendProbeRequest sends a probe request to the next TLB in the ring
func (tlb *L1TLB) SendProbeRequest(req *ProbeRequest) {
	nextTLB := tlb.Ring.GetNextTLB(tlb.ID, req.Direction)
	if nextTLB != nil && req.TTL > 0 {
		req.TTL--
		req.Src = tlb
		req.Dst = nextTLB
		req.SendTime = tlb.Ring.engine.CurrentTime()
		req.TrafficBytes = 64 // 64-bit probe request, per paper
		tlb.Ring.conn.Send(req)
	} else {
		sourceTLB := tlb.Ring.TLBs[req.SourceTLB]
		sourceTLB.NotifyMiss(req.VirtualAddr)
	}
}

// DeliverMessage delivers a probe request or response to the appropriate TLB
func (ring *RingNoC) DeliverMessage(msg sim.Msg, now sim.VTimeInSec) bool {
	switch msg := msg.(type) {
	case *ProbeRequest:
		msg.RecvTime = now
		tlb := ring.TLBs[msg.SourceTLB]
		tlb.ReceiveProbeRequest(msg, now)
		return true
	case *ProbeResponse:
		msg.RecvTime = now
		tlb := ring.TLBs[msg.SourceTLB]
		tlb.ReceiveProbeResponse(msg, now)
		return true
	}
	return false
}

// Cycle processes one probe request per TLB per cycle
func (ring *RingNoC) Cycle(now sim.VTimeInSec) bool {
	madeProgress := false
	for _, tlb := range ring.TLBs {
		if len(tlb.ProbeQueue) > 0 {
			req := tlb.ProbeQueue[0]
			tlb.ProbeQueue = tlb.ProbeQueue[1:]
			tlb.SendProbeRequest(&req)
			madeProgress = true
		}
	}
	ring.conn.Cycle(now)
	return madeProgress
}

// ReceiveProbeRequest processes an incoming probe request
func (tlb *L1TLB) ReceiveProbeRequest(req *ProbeRequest, now sim.VTimeInSec) {
	// Check TLB entries
	setID := tlb.vAddrToSetID(req.VirtualAddr)
	set := tlb.Sets[setID]
	wayID, page, found := set.Lookup(req.PID, req.VirtualAddr)
	if found && page.Valid {
		// Send response to source TLB
		rsp := &ProbeResponse{
			MsgMeta: sim.MsgMeta{
				ID:           sim.GetIDGenerator().Generate(),
				Src:          tlb,
				Dst:          tlb.Ring.TLBs[req.SourceTLB],
				SendTime:     now,
				TrafficClass: 0,
				TrafficBytes: 128, // 128-bit response (address + metadata)
			},
			VirtualAddr: req.VirtualAddr,
			Page:        &page,
			SourceTLB:   req.SourceTLB,
			SEID:        req.SEID,
		}
		tlb.Ring.conn.Send(rsp)
		return
	}

	// Check prefetch buffer
	for _, p := range tlb.PrefetchBuffer {
		if p.PID == req.PID && p.VAddr == req.VirtualAddr && p.Valid {
			rsp := &ProbeResponse{
				MsgMeta: sim.MsgMeta{
					ID:           sim.GetIDGenerator().Generate(),
					Src:          tlb,
					Dst:          tlb.Ring.TLBs[req.SourceTLB],
					SendTime:     now,
					TrafficClass: 0,
					TrafficBytes: 128,
				},
				VirtualAddr: req.VirtualAddr,
				Page:        &p,
				SourceTLB:   req.SourceTLB,
				SEID:        req.SEID,
			}
			tlb.Ring.conn.Send(rsp)
			return
		}
	}

	// Not found, forward to next TLB
	tlb.ProbeQueue = append(tlb.ProbeQueue, *req)
}

// ReceiveProbeResponse processes an incoming probe response
func (tlb *L1TLB) ReceiveProbeResponse(rsp *ProbeResponse, now sim.VTimeInSec) {
	if rsp.Page != nil && rsp.Page.Valid {
		// Translation found, update TLB
		setID := tlb.vAddrToSetID(rsp.VirtualAddr)
		set := tlb.Sets[setID]
		wayID, ok := set.Evict()
		if !ok {
			panic("failed to evict")
		}
		set.Update(wayID, *rsp.Page)
		set.Visit(wayID)

		// Find the original translation request in MSHR
		mshrEntry := tlb.mshr.GetEntry(rsp.Page.PID, rsp.VirtualAddr)
		if mshrEntry != nil {
			for _, req := range mshrEntry.Requests {
				// Send response to original requester
				translationRsp := vm.TranslationRspBuilder{}.
					WithSendTime(now).
					WithSrc(tlb.topPort).
					WithDst(req.Src).
					WithRspTo(req.ID).
					WithPage(*rsp.Page).
					Build()
				if err := tlb.topPort.Send(translationRsp); err == nil {
					tracing.TraceReqComplete(req, tlb)
				}
			}
			tlb.mshr.Remove(rsp.Page.PID, rsp.VirtualAddr)
		}
	} else {
		// No translation found, notify miss
		tlb.NotifyMiss(rsp.VirtualAddr)
	}
}

// NotifyMiss notifies the source TLB of a probe miss
func (tlb *L1TLB) NotifyMiss(virtualAddr uint64) {
	// Check MSHR for pending requests
	mshrEntry := tlb.mshr.Query(0, virtualAddr) // PID 0 for simplicity, adjust if needed
	if mshrEntry == nil {
		return // No pending request found
	}

	// Send translation request to L2 TLB
	for _, req := range mshrEntry.Requests {
		fetchReq := vm.TranslationReqBuilder{}.
			WithSendTime(tlb.Ring.engine.CurrentTime()).
			WithSrc(tlb.bottomPort).
			WithDst(tlb.LowModule).
			WithPID(req.PID).
			WithVAddr(virtualAddr).
			WithDeviceID(req.DeviceID).
			Build()
		if err := tlb.bottomPort.Send(fetchReq); err == nil {
			mshrEntry.reqToBottom = fetchReq
			tracing.TraceReqInitiate(fetchReq, tlb, tracing.MsgIDAtReceiver(req, tlb))
		}
	}
}

// InitiateProbing initiates probing for a virtual address
func (tlb *L1TLB) InitiateProbing(virtualAddr uint64) {
	clockwiseReq := &ProbeRequest{
		MsgMeta: sim.MsgMeta{
			ID:           sim.GetIDGenerator().Generate(),
			Src:          tlb,
			Dst:          nil,
			SendTime:     tlb.Ring.engine.CurrentTime(),
			TrafficClass: 0,
			TrafficBytes: 64, // 64-bit probe request, per paper
		},
		VirtualAddr: virtualAddr,
		TTL:         15,
		Direction:   "clockwise",
		SourceTLB:   tlb.ID,
		SEID:        tlb.Ring.SEID,
	}
	counterclockwiseReq := &ProbeRequest{
		MsgMeta: sim.MsgMeta{
			ID:           sim.GetIDGenerator().Generate(),
			Src:          tlb,
			Dst:          nil,
			SendTime:     tlb.Ring.engine.CurrentTime(),
			TrafficClass: 0,
			TrafficBytes: 64, // 64-bit probe request, per paper
		},
		VirtualAddr: virtualAddr,
		TTL:         4,
		Direction:   "counterclockwise",
		SourceTLB:   tlb.ID,
		SEID:        tlb.Ring.SEID,
	}
	tlb.ProbeQueue = append(tlb.ProbeQueue, *clockwiseReq, *counterclockwiseReq)
}
