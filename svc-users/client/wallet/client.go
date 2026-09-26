package wallet

import (
	"context"

	walletv1 "github.com/Muhmd95/Contracts/wallet/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"svc-users/internal/users"
)

type Client interface {
	CreateWallet(ctx context.Context, req *users.CreateWalletProxyRequest) (*users.CreateWalletProxyResponse, error)
	GetUserWallets(ctx context.Context, userID string) (*users.GetUserWalletsResponse, error)
	GetWalletBalance(ctx context.Context, walletID string) (*users.GetWalletBalanceProxyResponse, error)
	DeleteWallet(ctx context.Context, req *users.DeleteWalletRequest) error
	DeleteUserWallets(ctx context.Context, userID string) error
	Close() error
}

type grpcClient struct {
	client walletv1.WalletServiceClient
	conn   *grpc.ClientConn
}

func (c *grpcClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
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

func (c *grpcClient) CreateWallet(ctx context.Context, req *users.CreateWalletProxyRequest) (*users.CreateWalletProxyResponse, error) {
	protoReq := &walletv1.CreateWalletRequest{
		UserId:      req.UserID,
		PhoneNumber: req.PhoneNumber,
	}
	resp, err := c.client.CreateWallet(ctx, protoReq)
	if err != nil {
		return nil, err
	}
	return &users.CreateWalletProxyResponse{
		WalletID:  resp.WalletId,
		Balance:   resp.Balance,
		CreatedAt: resp.CreatedAt.AsTime(),
	}, nil
}

func (c *grpcClient) GetUserWallets(ctx context.Context, userID string) (*users.GetUserWalletsResponse, error) {
	protoReq := &walletv1.GetUserWalletsRequest{
		UserId: userID,
	}
	resp, err := c.client.GetUserWallets(ctx, protoReq)
	if err != nil {
		return nil, err
	}
	
	summaries := make([]users.WalletSummary, len(resp.Wallets))
	for i, w := range resp.Wallets {
		summaries[i] = users.WalletSummary{
			WalletID:     w.WalletId,
			PhoneNumber:  w.PhoneNumber,
			Balance:      w.Balance,
			CurrencyCode: w.CurrencyCode,
		}
	}
	
	return &users.GetUserWalletsResponse{
		Wallets: summaries,
		Count:   len(summaries),
	}, nil
}

func (c *grpcClient) GetWalletBalance(ctx context.Context, walletID string) (*users.GetWalletBalanceProxyResponse, error) {
	protoReq := &walletv1.GetWalletBalanceRequest{
		WalletId: walletID,
	}
	resp, err := c.client.GetWalletBalance(ctx, protoReq)
	if err != nil {
		return nil, err
	}
	return &users.GetWalletBalanceProxyResponse{
		WalletID:  resp.WalletId,
		Balance:   resp.Balance,
		UpdatedAt: resp.UpdatedAt.AsTime(),
	}, nil
}

func (c *grpcClient) DeleteWallet(ctx context.Context, req *users.DeleteWalletRequest) error {
	protoReq := &walletv1.DeleteWalletRequest{
		WalletId: req.WalletID,
	}
	_, err := c.client.DeleteWallet(ctx, protoReq)
	return err
}

func (c *grpcClient) DeleteUserWallets(ctx context.Context, userID string) error {
	protoReq := &walletv1.DeleteUserWalletsRequest{
		UserId: userID,
	}
	_, err := c.client.DeleteUserWallets(ctx, protoReq)
	return err
}
