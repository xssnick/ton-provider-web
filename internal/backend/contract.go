package backend

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/ton-provider-web/internal/backend/db"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-storage-provider/pkg/contract"
	"github.com/xssnick/tonutils-storage/provider"
	"math/big"
)

func (s *Service) getContractWithdrawData(bag *db.Bag, owner *address.Address) (*address.Address, *cell.Cell, error) {
	return contract.PrepareWithdrawalRequest(bag.RootHash, bag.MerkleHash, bag.FullSize, bag.PieceSize, owner)
}

func (s *Service) getContractDeployData(ctx context.Context, bag *db.Bag, owner *address.Address, providerKey []byte) (*provider.Offer, *address.Address, *cell.Cell, *cell.Cell, error) {
	sr, err := s.provider.GetStorageRates(ctx, providerKey, bag.FullSize)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get storage rates: %w", err)
	}

	off := provider.CalculateBestProviderOffer(&provider.ProviderRates{
		Available:        sr.Available,
		RatePerMBDay:     tlb.FromNanoTON(new(big.Int).SetBytes(sr.RatePerMBDay)),
		MinBounty:        tlb.FromNanoTON(new(big.Int).SetBytes(sr.MinBounty)),
		SpaceAvailableMB: sr.SpaceAvailableMB,
		MinSpan:          sr.MinSpan,
		MaxSpan:          sr.MaxSpan,
		Size:             bag.FullSize,
	})

	addr, si, body, err := contract.PrepareV1DeployData(bag.RootHash, bag.MerkleHash, bag.FullSize, bag.PieceSize, owner, []contract.ProviderV1{
		{
			Address:       address.NewAddress(0, 0, providerKey),
			MaxSpan:       off.Span,
			PricePerMBDay: tlb.FromNanoTON(off.RatePerMBNano),
		},
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to prepare deploy data: %w", err)
	}

	siCell, err := tlb.ToCell(si)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to convert si to cell: %w", err)
	}

	return &off, addr, siCell, body, nil
}

func (s *Service) calcContractAddr(bag *db.Bag, owner *address.Address) (*address.Address, error) {
	addr, _, _, err := contract.PrepareV1DeployData(bag.RootHash, bag.MerkleHash, bag.FullSize, bag.PieceSize, owner, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to calc contract addr: %w", err)
	}
	return addr, nil
}

func (s *Service) fetchContractInfo(ctx context.Context, bag *db.Bag, owner *address.Address, providerKey []byte) (tlb.Coins, uint64, tlb.Coins, error) {
	addr, _, _, err := contract.PrepareV1DeployData(bag.RootHash, bag.MerkleHash, bag.FullSize, bag.PieceSize, owner, nil)
	if err != nil {
		return tlb.ZeroCoins, 0, tlb.ZeroCoins, fmt.Errorf("failed to calc contract addr: %w", err)
	}

	master, err := s.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return tlb.ZeroCoins, 0, tlb.ZeroCoins, fmt.Errorf("failed to fetch master block: %w", err)
	}

	data, balance, err := contract.GetProviderDataV1(ctx, s.api, master, addr, providerKey)
	if err != nil {
		if errors.Is(err, contract.ErrNotDeployed) {
			return tlb.ZeroCoins, 0, tlb.ZeroCoins, contract.ErrNotDeployed
		}
		return tlb.ZeroCoins, 0, tlb.ZeroCoins, fmt.Errorf("failed to fetch providers list: %w", err)
	}

	szMB := new(big.Float).Quo(
		new(big.Float).SetUint64(bag.FullSize),
		big.NewFloat(1024*1024),
	)

	pricePerDay, _ := new(big.Float).Mul(szMB, new(big.Float).SetInt(data.RatePerMB.Nano())).Int(nil)

	return balance, data.ByteToProof, tlb.FromNanoTON(pricePerDay), nil
}
