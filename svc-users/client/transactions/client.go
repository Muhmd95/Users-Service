package transactions

import (
	"context"

	pb "github.com/Muhmd95/Contracts/transactions/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"svc-users/internal/users"
)

type Client struct {
	client pb.TransactionsServiceClient
	conn   *grpc.ClientConn
}

// Transactions is only reachable on the private service network, like Wallet.
func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, err
	}
	return &Client{client: pb.NewTransactionsServiceClient(conn), conn: conn}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Deposit(ctx context.Context, req *users.TransactionRequest, key string) (*users.TransactionResponse, error) {
	resp, err := c.client.Deposit(ctx, &pb.DepositRequest{
		PhoneNumber: req.PhoneNumber, Amount: req.Amount, IdempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	return &users.TransactionResponse{TransactionID: resp.TransactionId, WalletID: resp.WalletId,
		Status: resp.Status, Balance: resp.Balance, CreatedAt: resp.CreatedAt.AsTime()}, nil
}

func (c *Client) Withdraw(ctx context.Context, req *users.TransactionRequest, key string) (*users.TransactionResponse, error) {
	resp, err := c.client.Withdraw(ctx, &pb.WithdrawRequest{
		PhoneNumber: req.PhoneNumber, Amount: req.Amount, IdempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	return &users.TransactionResponse{TransactionID: resp.TransactionId, WalletID: resp.WalletId,
		Status: resp.Status, Balance: resp.Balance, CreatedAt: resp.CreatedAt.AsTime()}, nil
}

func (c *Client) Transfer(ctx context.Context, req *users.TransferRequest, key string) (*users.TransferResponse, error) {
	resp, err := c.client.Transfer(ctx, &pb.TransferRequest{
		SenderPhone: req.SenderPhone, ReceiverPhone: req.ReceiverPhone, Amount: req.Amount, IdempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	return &users.TransferResponse{TransactionID: resp.TransactionId, SenderWalletID: resp.SenderWalletId,
		Status: resp.Status, BalanceBefore: resp.BalanceBefore, BalanceAfter: resp.BalanceAfter}, nil
}
