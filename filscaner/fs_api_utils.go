package filscaner

import (
	errs "filscan_lotus/error"
	"filscan_lotus/utils"
	"fmt"
	"math/big"

	"filscan_lotus/models"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	b "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	p "github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/types"
	core "github.com/libp2p/go-libp2p-core"
)

func (fs *Filscaner) api_miner_state_at_tipset(miner_addr address.Address, tipset *types.TipSet) (*models.MinerStateAtTipset, error) {
	var (
		peerid              core.PeerID
		owner               address.Address
		power               api.MinerPower
		sectors             []*api.SectorInfo
		sector_size         uint64
		proving_sector_size = models.NewBigintFromInt64(0)
		err                 error
	)

	// TODO:把minerPeerId和MinerSectorSize缓存起来,可以减少2/6的lotus rpc访问量
	minerPower, err := fs.api.StateMinerPower(fs.ctx, miner_addr, tipset.Key())
	if err != nil {
		err_message := err.Error()
		if err_message == "failed to get miner power from chain (exit code 1)" {
			fs.Printf("get miner(%s) power failed, message:%s\n", miner_addr.String(), err_message)
			if power, err := fs.api.StateMinerPower(fs.ctx, address.Undef, tipset.Key()); err == nil {
				power.MinerPower = p.Claim{
					RawBytePower:    b.Zero(),
					QualityAdjPower: b.Zero(),
				}
			}
		}
		if err != nil {
			fs.Printf("get miner(%s) power failed, message:%s\n", miner_addr.String(), err.Error())
			return nil, err
		}
	}

	secCounts, err := fs.api.StateMinerSectorCount(fs.ctx, miner_addr, tipset.Key())
	if err != nil {
		fs.Printf("get state miner sector count failed, message:%s\n", err.Error())
		return nil, err
	}

	proving := secCounts.Active + secCounts.Faulty

	minerInfo, err := fs.api.StateMinerInfo(fs.ctx, miner_addr, tipset.Key())
	if err != nil {
		// fs.Printf("get peerid failed, address=%s message:%s\n", miner_addr.String(), err.Error())
	}

	sector_size = uint64(minerInfo.SectorSize)
	proving_sector_size.Set(big.NewInt(0).Mul(big.NewInt(int64(sector_size)), big.NewInt(int64(proving))))

	if fs.latest_total_power == nil {
		fs.latest_total_power = big.NewInt(0)
	}
	fs.latest_total_power.SetInt64(power.TotalPower.QualityAdjPower.Int64())

	miner := &models.MinerStateAtTipset{
		PeerId:            peerid.String(),
		MinerAddr:         miner_addr.String(),
		Power:             models.NewBigintFromInt64(power.MinerPower.QualityAdjPower.Int64()),
		TotalPower:        models.NewBigintFromInt64(power.TotalPower.QualityAdjPower.Int64()),
		SectorSize:        sector_size,
		WalletAddr:        owner.String(),
		SectorCount:       uint64(len(sectors)),
		TipsetHeight:      uint64(tipset.Height()),
		ProvingSectorSize: proving_sector_size,
		MineTime:          tipset.MinTimestamp(),
	}

	miner.SectorCount = uint64(len(sectors))

	return miner, nil
}

func (fs *Filscaner) api_tipset(tpstk string) (*types.TipSet, error) {
	tipsetk := utils.Tipsetkey_from_string(tpstk)
	if tipsetk == nil {
		return nil, fmt.Errorf("convert string(%s) to tipsetkey failed", tpstk)
	}

	return fs.api.ChainGetTipSet(fs.ctx, *tipsetk)
}

func (fs *Filscaner) api_child_tipset(tipset *types.TipSet) (*types.TipSet, error) {
	if tipset == nil {
		return nil, nil
	}

	fs.mutx_for_numbers.Lock()
	var header_height = fs.header_height
	fs.mutx_for_numbers.Unlock()

	for i := uint64(tipset.Height()) + 1; i < header_height; i++ {
		if child, err := fs.api.ChainGetTipSetByHeight(fs.ctx, abi.ChainEpoch(i), types.EmptyTSK); err != nil {
			return nil, err
		} else {
			if child.Parents().String() == tipset.Key().String() {
				return child, nil
			} else {
				return nil, fmt.Errorf("child(%d)'s parent key(%s) != key(%d, %s)\n",
					child.Height(), child.Parents().String(),
					tipset.Height(), tipset.Key().String())
			}

		}
	}
	return nil, errs.ErrNotFound
}

func (fs *Filscaner) API_block_rewards(actorAddress address.Address, tipset *types.TipSet) *big.Int {
	actor, err := fs.api.StateGetActor(fs.ctx, actorAddress, tipset.Key())
	if err != nil {
		return nil
	}
	balance := types.NewInt(actor.Balance.Uint64())
	reward := models.MiningReward(balance)
	return reward.Int
}
