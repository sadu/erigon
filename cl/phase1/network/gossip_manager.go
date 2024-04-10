package network

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/gointerfaces/grpcutil"
	"github.com/ledgerwatch/erigon/cl/beacon/beaconevents"
	"github.com/ledgerwatch/erigon/cl/cltypes/solid"
	"github.com/ledgerwatch/erigon/cl/gossip"
	"github.com/ledgerwatch/erigon/cl/phase1/forkchoice"
	"github.com/ledgerwatch/erigon/cl/phase1/network/services"
	"github.com/ledgerwatch/erigon/cl/validator/committee_subscription"
	"google.golang.org/grpc"

	"github.com/ledgerwatch/erigon-lib/gointerfaces/sentinel"
	"github.com/ledgerwatch/erigon-lib/types/ssz"
	"github.com/ledgerwatch/erigon/cl/clparams"
	"github.com/ledgerwatch/erigon/cl/cltypes"
	"github.com/ledgerwatch/erigon/cl/utils"
	"github.com/ledgerwatch/log/v3"
)

// Gossip manager is sending all messages to fork choice or others
type GossipManager struct {
	forkChoice *forkchoice.ForkChoiceStore
	sentinel   sentinel.SentinelClient
	// configs
	beaconConfig  *clparams.BeaconChainConfig
	genesisConfig *clparams.GenesisConfig

	emitters     *beaconevents.Emitters
	committeeSub *committee_subscription.CommitteeSubscribeMgmt

	// Services for processing messages from the network
	blockService services.BlockService
	blobService  services.BlobSidecarsService
}

func NewGossipReceiver(
	s sentinel.SentinelClient,
	forkChoice *forkchoice.ForkChoiceStore,
	beaconConfig *clparams.BeaconChainConfig,
	genesisConfig *clparams.GenesisConfig,
	emitters *beaconevents.Emitters,
	comitteeSub *committee_subscription.CommitteeSubscribeMgmt,
	blockService services.BlockService,
	blobService services.BlobSidecarsService,
) *GossipManager {
	return &GossipManager{
		sentinel:      s,
		forkChoice:    forkChoice,
		emitters:      emitters,
		beaconConfig:  beaconConfig,
		genesisConfig: genesisConfig,
		committeeSub:  comitteeSub,
		blockService:  blockService,
		blobService:   blobService,
	}
}

func operationsContract[T ssz.EncodableSSZ](ctx context.Context, g *GossipManager, l log.Ctx, data *sentinel.GossipData, version int, name string, fn func(T, bool) error) error {
	var t T
	object := t.Clone().(T)
	if err := object.DecodeSSZ(common.CopyBytes(data.Data), version); err != nil {
		g.sentinel.BanPeer(ctx, data.Peer)
		l["at"] = fmt.Sprintf("decoding %s", name)
		return err
	}
	if err := fn(object /*test=*/, false); err != nil {
		l["at"] = fmt.Sprintf("verify %s", name)
		return err
	}
	if _, err := g.sentinel.PublishGossip(ctx, data); err != nil {
		log.Debug("failed publish gossip", "err", err)
	}
	return nil
}

func (g *GossipManager) onRecv(ctx context.Context, data *sentinel.GossipData, l log.Ctx) (err error) {
	// defer func() {
	// 	r := recover()
	// 	if r != nil {
	// 		err = fmt.Errorf("%v", r)
	// 	}
	// }()
	// Make a copy of the gossip data so that we the received data is not modified.
	// 1) When we publish and corrupt the data, the peers bans us.
	// 2) We decode the block wrong
	data = &sentinel.GossipData{
		Name:     data.Name,
		Peer:     data.Peer,
		SubnetId: data.SubnetId,
		Data:     common.CopyBytes(data.Data),
	}

	currentEpoch := utils.GetCurrentEpoch(g.genesisConfig.GenesisTime, g.beaconConfig.SecondsPerSlot, g.beaconConfig.SlotsPerEpoch)
	version := g.beaconConfig.GetCurrentStateVersion(currentEpoch)

	// Depending on the type of the received data, we create an instance of a specific type that implements the ObjectSSZ interface,
	// then attempts to deserialize the received data into it.
	// If the deserialization fails, an error is logged and the loop returns to the next iteration.
	// If the deserialization is successful, the object is set to the deserialized value and the loop returns to the next iteration.
	switch data.Name {
	case gossip.TopicNameBeaconBlock:
		obj := cltypes.NewSignedBeaconBlock(g.beaconConfig)
		if err := obj.DecodeSSZ(data.Data, int(version)); err != nil {
			g.sentinel.BanPeer(ctx, data.Peer)
			l["at"] = "decoding block"
			return err
		}
		log.Debug("Received block via gossip", "slot", obj.Block.Slot)
		err = g.blockService.ProcessMessage(ctx, data.SubnetId, obj)
	case gossip.TopicNameSyncCommitteeContributionAndProof:
		if err := operationsContract[*cltypes.SignedContributionAndProof](ctx, g, l, data, int(version), "contribution and proof", g.forkChoice.OnSignedContributionAndProof); err != nil {
			return err
		}
	case gossip.TopicNameVoluntaryExit:
		if err := operationsContract[*cltypes.SignedVoluntaryExit](ctx, g, l, data, int(version), "voluntary exit", g.forkChoice.OnVoluntaryExit); err != nil {
			return err
		}
	case gossip.TopicNameProposerSlashing:
		if err := operationsContract[*cltypes.ProposerSlashing](ctx, g, l, data, int(version), "proposer slashing", g.forkChoice.OnProposerSlashing); err != nil {
			return err
		}
	case gossip.TopicNameAttesterSlashing:
		if err := operationsContract[*cltypes.AttesterSlashing](ctx, g, l, data, int(version), "attester slashing", g.forkChoice.OnAttesterSlashing); err != nil {
			return err
		}
	case gossip.TopicNameBlsToExecutionChange:
		if err := operationsContract[*cltypes.SignedBLSToExecutionChange](ctx, g, l, data, int(version), "bls to execution change", g.forkChoice.OnBlsToExecutionChange); err != nil {
			return err
		}
	case gossip.TopicNameBeaconAggregateAndProof:
		if err := operationsContract[*cltypes.SignedAggregateAndProof](ctx, g, l, data, int(version), "aggregate and proof", g.forkChoice.OnAggregateAndProof); err != nil {
			return err
		}

	default:
		switch {
		case gossip.IsTopicBlobSidecar(data.Name):
			// decode sidecar
			blobSideCar := &cltypes.BlobSidecar{}
			if err := blobSideCar.DecodeSSZ(data.Data, int(version)); err != nil {
				g.sentinel.BanPeer(ctx, data.Peer)
				l["at"] = "decoding blob sidecar"
				return err
			}

			// The background checks above are enough for now.
			err = g.blobService.ProcessMessage(ctx, data.SubnetId, blobSideCar)
			log.Debug("Received blob sidecar via gossip", "index", *data.SubnetId, "size", datasize.ByteSize(len(blobSideCar.Blob)))
		case gossip.IsTopicSyncCommittee(data.Name):
			if data.SubnetId == nil {
				return fmt.Errorf("missing subnet id")
			}
			msg := &cltypes.SyncCommitteeMessage{}
			if err := msg.DecodeSSZ(common.CopyBytes(data.Data), int(version)); err != nil {
				g.sentinel.BanPeer(ctx, data.Peer)
				l["at"] = "decoding sync committee message"
				return err
			}
			if err := g.forkChoice.OnSyncCommitteeMessage(msg, *data.SubnetId); err != nil {
				g.sentinel.BanPeer(ctx, data.Peer)
				l["at"] = "on sync committee message"
				return err
			}
		case gossip.IsTopicBeaconAttestation(data.Name):
			att := &solid.Attestation{}
			if err := att.DecodeSSZ(common.CopyBytes(data.Data), int(version)); err != nil {
				g.sentinel.BanPeer(ctx, data.Peer)
				l["at"] = "decoding attestation"
				return err
			}
			if err := g.forkChoice.OnCheckReceivedAttestation(data.Name, att); err != nil {
				log.Debug("failed to check attestation", "err", err)
				if errors.Is(err, forkchoice.ErrIgnore) {
					return nil
				}
				g.sentinel.BanPeer(ctx, data.Peer)
				return err
			}
			// check if it needs to be aggregated
			if err := g.committeeSub.CheckAggregateAttestation(att); err != nil {
				log.Debug("failed to check aggregate attestation", "err", err)
			}
			// publish
			if _, err := g.sentinel.PublishGossip(ctx, data); err != nil {
				log.Debug("failed publish gossip", "err", err)
			}
		default:
		}
	}
	if errors.Is(err, services.ErrIgnore) {
		return nil
	}
	if err != nil {
		l["at"] = "block service"
		g.sentinel.BanPeer(ctx, data.Peer)
		return err
	}
	if _, err := g.sentinel.PublishGossip(ctx, data); err != nil {
		log.Debug("failed publish gossip", "err", err)
	}
	return nil
}

func (g *GossipManager) Start(ctx context.Context) {
	operationsCh := make(chan *sentinel.GossipData, 1<<16)
	blobsCh := make(chan *sentinel.GossipData, 1<<16)
	blocksCh := make(chan *sentinel.GossipData, 1<<16)
	syncCommitteesCh := make(chan *sentinel.GossipData, 1<<16)
	defer close(operationsCh)
	defer close(blobsCh)
	defer close(blocksCh)

	// Start a goroutine that listens for new gossip messages and sends them to the operations processor.
	goWorker := func(ch <-chan *sentinel.GossipData, workerCount int) {
		worker := func() {
			for {
				select {
				case <-ctx.Done():
					return
				case data := <-ch:
					l := log.Ctx{}
					if err := g.onRecv(ctx, data, l); err != nil {
						log.Debug("[Beacon Gossip] Recoverable Error", "err", err)
					}
				}
			}
		}
		for i := 0; i < workerCount; i++ {
			go worker()
		}
	}
	goWorker(operationsCh, 1)
	goWorker(blocksCh, 1)
	goWorker(blobsCh, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-syncCommitteesCh:
				l := log.Ctx{}
				err := g.onRecv(ctx, data, l)
				if err != nil {
					log.Warn("[Beacon Gossip] Recoverable Error", "err", err)
				}
			}
		}
	}()

Reconnect:
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		subscription, err := g.sentinel.SubscribeGossip(ctx, &sentinel.SubscriptionData{}, grpc.WaitForReady(true))
		if err != nil {
			return
		}

		for {
			data, err := subscription.Recv()
			if err != nil {
				if grpcutil.IsRetryLater(err) || grpcutil.IsEndOfStream(err) {
					time.Sleep(3 * time.Second)
					continue Reconnect
				}
				log.Warn("[Beacon Gossip] Fatal error receiving gossip", "err", err)
				continue Reconnect
			}

			if data.Name == gossip.TopicNameBeaconBlock || gossip.IsTopicBlobSidecar(data.Name) {
				blocksCh <- data
			} else if gossip.IsTopicSyncCommittee(data.Name) || data.Name == gossip.TopicNameSyncCommitteeContributionAndProof {
				syncCommitteesCh <- data
			} else {
				operationsCh <- data
			}
		}
	}
}
