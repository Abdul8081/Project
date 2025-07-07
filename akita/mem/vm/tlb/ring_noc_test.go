package tlb

import (
	"testing"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
)

func TestRingNoCFullProbeFlow(t *testing.T) {
	engine := sim.NewSerialEngine()
	tlbBuilder := MakeBuilder().
		WithEngine(engine).
		WithFreq(1 * sim.GHz).
		WithNumMSHREntry(4).
		WithNumSets(1).
		WithNumWays(64).
		WithNumReqPerCycle(4)

	ring := NewRingNoC("TestRing", engine, 0, tlbBuilder)

	// Setup TLB 1 with a valid page
	page := vm.Page{
		PID:   0,
		VAddr: 0x1000,
		PAddr: 0x2000,
		Valid: true,
	}
	tlb1 := ring.TLBs[1]
	setID := tlb1.vAddrToSetID(0x1000)
	tlb1.Sets[setID].Update(0, page)

	// Create a translation request for TLB 0
	req := vm.TranslationReqBuilder{}.
		WithPID(0).
		WithVAddr(0x1000).
		WithDeviceID(1).
		Build()
	tlb0 := ring.TLBs[0]
	tlb0.mshr.Add(0, 0x1000).Requests = append(tlb0.mshr.GetEntry(0, 0x1000).Requests, req)

	// Initiate probing from TLB 0
	tlb0.InitiateProbing(0x1000)

	// Simulate cycles to process probe queue
	for i := 0; i < 10; i++ {
		ring.Cycle(sim.VTimeInSec(i) + 1.0)
	}

	// Verify TLB 0 received the translation
	setID = tlb0.vAddrToSetID(0x1000)
	_, foundPage, found := tlb0.Sets[setID].Lookup(0, 0x1000)
	if !found || !foundPage.Valid || foundPage.PAddr != 0x2000 {
		t.Errorf("Expected TLB 0 to have valid page for 0x1000, got found=%v, valid=%v, paddr=0x%x",
			found, foundPage.Valid, foundPage.PAddr)
	}

	// Verify MSHR is cleared
	if tlb0.mshr.Query(0, 0x1000) != nil {
		t.Errorf("Expected MSHR to be cleared for 0x1000")
	}
}

func TestRingNoCProbeMiss(t *testing.T) {
	engine := sim.NewSerialEngine()
	tlbBuilder := MakeBuilder().
		WithEngine(engine).
		WithFreq(1 * sim.GHz).
		WithNumMSHREntry(4).
		WithNumSets(1).
		WithNumWays(64).
		WithNumReqPerCycle(4)

	ring := NewRingNoC("TestRing", engine, 0, tlbBuilder)
	tlb0 := ring.TLBs[0]

	// Create a translation request
	req := vm.TranslationReqBuilder{}.
		WithPID(0).
		WithVAddr(0x1000).
		WithDeviceID(1).
		Build()
	tlb0.mshr.Add(0, 0x1000).Requests = append(tlb0.mshr.GetEntry(0, 0x1000).Requests, req)

	// Mock L2 TLB port
	mockL2Port := sim.NewLimitNumMsgPort(nil, 4, "MockL2Port")
	tlb0.LowModule = mockL2Port

	// Initiate probing (no TLB has the page)
	tlb0.InitiateProbing(0x1000)

	// Simulate cycles to process probe queue (TTL expires)
	now := sim.VTimeInSec(1.0)
	for i := 0; i < 20; i++ {
		ring.Cycle(now + sim.VTimeInSec(i))
	}

	// Collect messages from mock L2 port
	var messages []sim.Msg
	for {
		msg := mockL2Port.Retrieve(now + sim.VTimeInSec(20))
		if msg == nil {
			break
		}
		messages = append(messages, msg)
	}

	// Verify L2 TLB request was sent
	if len(messages) == 0 {
		t.Errorf("Expected L2 TLB request to be sent")
	} else {
		found := false
		for _, msg := range messages {
			if transReq, ok := msg.(*vm.TranslationReq); ok {
				if transReq.VAddr == 0x1000 && transReq.PID == 0 {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("Expected L2 request for vaddr=0x1000, pid=0")
		}
	}

	// Verify MSHR still has the entry with reqToBottom
	mshrEntry := tlb0.mshr.Query(0, 0x1000)
	if mshrEntry == nil || mshrEntry.reqToBottom == nil {
		t.Errorf("Expected MSHR entry with reqToBottom for 0x1000")
	}
}

func TestRingNoCDeliverMessage(t *testing.T) {
	engine := sim.NewSerialEngine()
	tlbBuilder := MakeBuilder().
		WithEngine(engine).
		WithFreq(1 * sim.GHz).
		WithNumMSHREntry(4).
		WithNumSets(1).
		WithNumWays(64).
		WithNumReqPerCycle(4)
	ring := NewRingNoC("TestRing", engine, 0, tlbBuilder)
	req := &ProbeRequest{
		MsgMeta: sim.MsgMeta{
			ID:           sim.GetIDGenerator().Generate(),
			Src:          ring.TLBs[0].topPort,  // Use topPort instead of TLB
			Dst:          ring.TLBs[15].topPort, // Use topPort instead of TLB
			SendTime:     1.0,
			TrafficBytes: 64,
		},
		VirtualAddr: 0x1000,
		TTL:         4,
		Direction:   "counterclockwise",
		SourceTLB:   0,
		SEID:        0,
	}
	if !ring.DeliverMessage(req, 1.0) {
		t.Error("Expected DeliverMessage to return true for ProbeRequest")
	}

	rsp := &ProbeResponse{
		MsgMeta: sim.MsgMeta{
			ID:           sim.GetIDGenerator().Generate(),
			Src:          ring.TLBs[15].topPort, // Use topPort instead of TLB
			Dst:          ring.TLBs[0].topPort,  // Use topPort instead of TLB
			SendTime:     1.0,
			TrafficBytes: 128,
		},
		VirtualAddr: 0x1000,
		Page:        nil,
		SourceTLB:   0,
		SEID:        0,
	}
	if !ring.DeliverMessage(rsp, 1.0) {
		t.Error("Expected DeliverMessage to return true for ProbeResponse")
	}
}
