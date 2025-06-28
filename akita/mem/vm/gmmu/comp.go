// Package gmmu provides the implementation of the Graphics Memory Management Unit (GMMU).
// It includes structures and methods for handling memory translation, page migration,
// and other related operations within the virtual memory system.
package gmmu

import (
	"encoding/binary"
	"log"
	"reflect"
	"sync"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/akita/v3/tracing"
	cuckoo "github.com/seiflotfy/cuckoofilter"
)

type transaction struct {
	req       *vm.TranslationReq
	page      vm.Page
	cycleLeft int
}

// Comp is the default gmmu implementation. It is also an akita Component.
type Comp struct {
	sim.TickingComponent

	deviceID uint64

	topPort    sim.Port
	bottomPort sim.Port
	LowModule  sim.Port

	topSender    sim.BufferedSender
	bottomSender sim.BufferedSender

	pageTable           vm.PageTable
	latency             int
	maxRequestsInFlight int

	walkingTranslations []transaction
	remoteMemReqs       map[uint64]transaction

	toRemoveFromPTW        []int
	PageAccessedByDeviceID map[uint64][]uint64
	cuckooFilter           *cuckoo.Filter // Cuckoo filter for fast lookup
	cuckooMutex            sync.Mutex     // Mutex for thread-safe filter access my change
}

// Tick defines how the gmmu update state each cycle
func (gmmu *Comp) Tick(now sim.VTimeInSec) bool {
	madeProgress := false

	madeProgress = gmmu.topSender.Tick(now) || madeProgress
	madeProgress = gmmu.parseFromTop(now) || madeProgress
	madeProgress = gmmu.walkPageTable(now) || madeProgress
	madeProgress = gmmu.fetchFromBottom(now) || madeProgress

	return madeProgress
}

// my change incode VAddr and PID to cuckoo filter implementation like insert, delete, lookup
func (gmmu *Comp) encodeVAddrPID(vAddr uint64, pid vm.PID) []byte {
	buf := make([]byte, 12) // 8 bytes for vAddr + 4 bytes for PID
	binary.LittleEndian.PutUint64(buf[0:8], vAddr)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(pid))
	return buf
}

func (gmmu *Comp) parseFromTop(now sim.VTimeInSec) bool {
	if len(gmmu.walkingTranslations) >= gmmu.maxRequestsInFlight {
		return false
	}

	req := gmmu.topPort.Retrieve(now)
	if req == nil {
		return false
	}

	tracing.TraceReqReceive(req, gmmu)

	switch req := req.(type) {
	case *vm.TranslationReq:
		// Check Cuckoo filter my change
		gmmu.cuckooMutex.Lock()
		found := gmmu.cuckooFilter.Lookup(gmmu.encodeVAddrPID(req.VAddr, req.PID))
		gmmu.cuckooMutex.Unlock()
		if found {
			// Verify with page table to handle false positives
			page, found := gmmu.pageTable.Find(req.PID, req.VAddr)
			if found && page.DeviceID == gmmu.deviceID {
				if gmmu.topSender.CanSend(1) {
					rsp := vm.TranslationRspBuilder{}.
						WithSendTime(now).
						WithSrc(gmmu.topPort).
						WithDst(req.Src).
						WithRspTo(req.ID).
						WithPage(page).
						Build()
					gmmu.topSender.Send(rsp)
					tracing.TraceReqComplete(req, gmmu)
					return true
				}
			}
		}
		// Mapping not found or false positive, start page table walk
		gmmu.startWalking(req)
	default:
		log.Panicf("gmmu cannot handle request of type %s", reflect.TypeOf(req))
	}

	return true
}

func (gmmu *Comp) startWalking(req *vm.TranslationReq) {
	translationInPipeline := transaction{
		req:       req,
		cycleLeft: gmmu.latency,
	}

	gmmu.walkingTranslations = append(gmmu.walkingTranslations, translationInPipeline)
}

func (gmmu *Comp) walkPageTable(now sim.VTimeInSec) bool {
	madeProgress := false
	for i := 0; i < len(gmmu.walkingTranslations); i++ {
		if gmmu.walkingTranslations[i].cycleLeft > 0 {
			gmmu.walkingTranslations[i].cycleLeft--
			madeProgress = true
			continue
		}
		req := gmmu.walkingTranslations[i].req

		page, _ := gmmu.pageTable.Find(req.PID, req.VAddr)

		if page.DeviceID == gmmu.deviceID {
			madeProgress = gmmu.finalizePageWalk(now, i) || madeProgress
		} else {
			madeProgress = gmmu.processRemoteMemReq(now, i) || madeProgress
		}
	}

	tmp := gmmu.walkingTranslations[:0]
	for i := 0; i < len(gmmu.walkingTranslations); i++ {
		if !gmmu.toRemove(i) {
			tmp = append(tmp, gmmu.walkingTranslations[i])
		}
	}
	gmmu.walkingTranslations = tmp
	gmmu.toRemoveFromPTW = nil

	return madeProgress
}

func (gmmu *Comp) processRemoteMemReq(now sim.VTimeInSec, walkingIndex int) bool {
	// if !gmmu.bottomSender.CanSend(1) {
	// 	return false
	// }

	walking := gmmu.walkingTranslations[walkingIndex].req

	gmmu.remoteMemReqs[walking.VAddr] = gmmu.walkingTranslations[walkingIndex]

	req := vm.TranslationReqBuilder{}.
		WithSendTime(now).
		WithSrc(gmmu.bottomPort).
		WithDst(gmmu.LowModule).
		WithPID(walking.PID).
		WithVAddr(walking.VAddr).
		WithDeviceID(walking.DeviceID).
		Build()

	err := gmmu.bottomPort.Send(req)

	if err != nil {
		return false
	}

	gmmu.toRemoveFromPTW = append(gmmu.toRemoveFromPTW, walkingIndex)

	return true
}

func (gmmu *Comp) finalizePageWalk(
	now sim.VTimeInSec,
	walkingIndex int,
) bool {
	req := gmmu.walkingTranslations[walkingIndex].req
	page, _ := gmmu.pageTable.Find(req.PID, req.VAddr)
	gmmu.walkingTranslations[walkingIndex].page = page

	return gmmu.doPageWalkHit(now, walkingIndex)
}

func (gmmu *Comp) doPageWalkHit(
	now sim.VTimeInSec,
	walkingIndex int,
) bool {
	if !gmmu.topSender.CanSend(1) {
		return false
	}
	walking := gmmu.walkingTranslations[walkingIndex]

	rsp := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(gmmu.topPort).
		WithDst(walking.req.Src).
		WithRspTo(walking.req.ID).
		WithPage(walking.page).
		Build()

	gmmu.topSender.Send(rsp)

	gmmu.toRemoveFromPTW = append(gmmu.toRemoveFromPTW, walkingIndex)

	tracing.TraceReqComplete(walking.req, gmmu)

	return true
}

func (gmmu *Comp) toRemove(index int) bool {
	for i := 0; i < len(gmmu.toRemoveFromPTW); i++ {
		remove := gmmu.toRemoveFromPTW[i]
		if remove == index {
			return true
		}
	}
	return false
}

func (gmmu *Comp) fetchFromBottom(now sim.VTimeInSec) bool {
	if !gmmu.topSender.CanSend(1) {
		return false
	}

	req := gmmu.bottomPort.Retrieve(now)
	if req == nil {
		return false
	}

	tracing.TraceReqReceive(req, gmmu)

	switch req := req.(type) {
	case *vm.TranslationRsp:
		return gmmu.handleTranslationRsp(now, req)
	default:
		log.Panicf("gmmu canot handle request of type %s", reflect.TypeOf(req))
	}

	return true
}

func (gmmu *Comp) handleTranslationRsp(now sim.VTimeInSec, response *vm.TranslationRsp) bool {
	reqTransaction := gmmu.remoteMemReqs[response.Page.VAddr]

	// Update page table my change
	gmmu.pageTable.Update(response.Page)

	// Insert into Cuckoo filter my change
	gmmu.cuckooMutex.Lock()
	if !gmmu.cuckooFilter.Insert(gmmu.encodeVAddrPID(response.Page.VAddr, response.Page.PID)) {
		log.Printf("Warning: Failed to insert VAddr %d, PID %d into Cuckoo filter", response.Page.VAddr, response.Page.PID)
		gmmu.cuckooFilter.Reset()
		gmmu.cuckooFilter.Insert(gmmu.encodeVAddrPID(response.Page.VAddr, response.Page.PID))
	}
	gmmu.cuckooMutex.Unlock()

	rsp := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(gmmu.topPort).
		WithDst(reqTransaction.req.Src).
		WithRspTo(response.ID).
		WithPage(response.Page).
		Build()

	gmmu.topSender.Send(rsp)

	delete(gmmu.remoteMemReqs, response.Page.VAddr)
	return true
}
