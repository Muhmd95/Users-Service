package wallet

import (
	"context"

	walletv1 "github.com/Muhmd95/Contracts/wallet/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client interface {
	CreateWallet(ctx context.Context, userID, phoneNumber string) (string, error)
	GetUserWallets(ctx context.Context, userID string) ([]*walletv1.WalletInfo, error)
	GetWalletBalance(ctx context.Context, walletID string) (int64, error)
	DeleteWallet(ctx context.Context, userID, walletID string) error
	DeleteUserWallets(ctx context.Context, userID string) error
}

type grpcClient struct {
	client walletv1.WalletServiceClient
	conn   *grpc.ClientConn
}

func NewClient(addr string) (Client, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &grpcClient{
		client: walletv1.NewWalletServiceClient(conn),
		conn:   conn,
	}, nil
}

func (c *grpcClient) CreateWallet(ctx context.Context, userID, phoneNumber string) (string, error) {
	req := &walletv1.CreateWalletRequest{
		UserId:      userID,
		PhoneNumber: phoneNumber,
	}
	resp, err := c.client.CreateWallet(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.WalletId, nil
}

func (c *grpcClient) GetUserWallets(ctx context.Context, userID string) ([]*walletv1.WalletInfo, error) {
	req := &walletv1.GetUserWalletsRequest{
		UserId: userID,
	}
	resp, err := c.client.GetUserWallets(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Wallets, nil
}

func (c *grpcClient) GetWalletBalance(ctx context.Context, walletID string) (int64, error) {
	req := &walletv1.GetWalletBalanceRequest{
		WalletId: walletID,
	}
	resp, err := c.client.GetWalletBalance(ctx, req)
	if err != nil {
		return 0, err
	}
	return resp.Balance, nil
}

func (c *grpcClient) DeleteWallet(ctx context.Context, userID, walletID string) error {
	req := &walletv1.DeleteWalletRequest{
		UserId:   userID,
		WalletId: walletID,
	}
	_, err := c.client.DeleteWallet(ctx, req)
	return err
}

func (c *grpcClient) DeleteUserWallets(ctx context.Context, userID string) error {
	req := &walletv1.DeleteUserWalletsRequest{
		UserId: userID,
	}
	_, err := c.client.DeleteUserWallets(ctx, req)
	return err
}
