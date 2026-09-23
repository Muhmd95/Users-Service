package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"svc-users/internal/users"
	"svc-users/util/logger"
)

type mongoRepository struct {
	usersColl  *mongo.Collection
	tokensColl *mongo.Collection
}

func NewUsersRepository(ctx context.Context, db *mongo.Database) (users.Repository, error) {
	log := logger.Ctx(ctx)
	usersColl := db.Collection("users")
	tokensColl := db.Collection("refresh_tokens")

	// --- Indexes for Users Collection ---
	userIndexModels := []mongo.IndexModel{
		{
			Keys:    bson.M{"national_id": 1},
			Options: options.Index().SetUnique(true).SetName("unique_national_id"),
		},
		{
			Keys:    bson.M{"email": 1},
			Options: options.Index().SetUnique(true).SetName("unique_email"),
		},
		{
			Keys:    bson.M{"phone_numbers": 1}, // Creates a unique multikey index
			Options: options.Index().SetUnique(true).SetName("unique_phone_numbers"),
		},
	}

	_, err := usersColl.Indexes().CreateMany(ctx, userIndexModels)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create unique indexes on users collection")
		return nil, err
	}

	// --- Indexes for Refresh Tokens Collection ---
	tokenIndexModels := []mongo.IndexModel{
		{
			Keys:    bson.M{"token_hash": 1},
			Options: options.Index().SetUnique(true).SetName("unique_token_hash"),
		},
		{
			Keys:    bson.M{"user_id": 1},
			Options: options.Index().SetName("user_id_idx"),
		},
		{
			Keys:    bson.M{"expires_at": 1},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires_at"),
		},
	}

	_, err = tokensColl.Indexes().CreateMany(ctx, tokenIndexModels)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create indexes on refresh_tokens collection")
		return nil, err
	}

	return &mongoRepository{
		usersColl:  usersColl,
		tokensColl: tokensColl,
	}, nil
}

// --- Users Collection Methods ---

func (r *mongoRepository) CreateUser(ctx context.Context, user *users.User) error {
	log := logger.Ctx(ctx)
	result, err := r.usersColl.InsertOne(ctx, user)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return err // Return raw error so service can check which unique index failed
		}
		log.Error().Err(err).Msg("Failed to insert user")
		return err
	}
	user.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *mongoRepository) GetUserByID(ctx context.Context, userID string) (*users.User, error) {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, users.ErrInvalidUserID
	}
	var u users.User
	err = r.usersColl.FindOne(ctx, bson.M{"_id": objID}).Decode(&u)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, users.ErrUserNotFound
		}
		log.Error().Err(err).Msg("Failed to find user by ID")
		return nil, err
	}
	return &u, nil
}

func (r *mongoRepository) GetUserByPhoneNumber(ctx context.Context, phoneNumber string) (*users.User, error) {
	log := logger.Ctx(ctx)
	var u users.User
	err := r.usersColl.FindOne(ctx, bson.M{"phone_numbers": phoneNumber}).Decode(&u)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, users.ErrUserNotFound
		}
		log.Error().Err(err).Msg("Failed to find user by phone number")
		return nil, err
	}
	return &u, nil
}

func (r *mongoRepository) GetUserByEmail(ctx context.Context, email string) (*users.User, error) {
	log := logger.Ctx(ctx)
	var u users.User
	err := r.usersColl.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, users.ErrUserNotFound
		}
		log.Error().Err(err).Msg("Failed to find user by email")
		return nil, err
	}
	return &u, nil
}

func (r *mongoRepository) GetUserByNationalID(ctx context.Context, nationalID string) (*users.User, error) {
	log := logger.Ctx(ctx)
	var u users.User
	err := r.usersColl.FindOne(ctx, bson.M{"national_id": nationalID}).Decode(&u)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, users.ErrUserNotFound
		}
		log.Error().Err(err).Msg("Failed to find user by national ID")
		return nil, err
	}
	return &u, nil
}

func (r *mongoRepository) UpdateUser(ctx context.Context, userID string, updates map[string]interface{}) (*users.User, error) {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, users.ErrInvalidUserID
	}
	
	updates["updated_at"] = time.Now()
	updateDoc := bson.M{"$set": updates}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	
	var u users.User
	err = r.usersColl.FindOneAndUpdate(ctx, bson.M{"_id": objID}, updateDoc, opts).Decode(&u)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, users.ErrUserNotFound
		}
		log.Error().Err(err).Msg("Failed to update user")
		return nil, err
	}
	return &u, nil
}

func (r *mongoRepository) SoftDeleteUser(ctx context.Context, userID string) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	updateDoc := bson.M{
		"$set": bson.M{
			"status": "DELETED",
			"updated_at": time.Now(),
		},
	}
	
	result, err := r.usersColl.UpdateByID(ctx, objID, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to soft delete user")
		return err
	}
	if result.MatchedCount == 0 {
		return users.ErrUserNotFound
	}
	return nil
}

func (r *mongoRepository) ReactivateUser(ctx context.Context, userID string, updates map[string]interface{}) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	updates["status"] = "ACTIVE"
	updates["updated_at"] = time.Now()
	
	updateDoc := bson.M{"$set": updates}
	result, err := r.usersColl.UpdateByID(ctx, objID, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to reactivate user")
		return err
	}
	if result.MatchedCount == 0 {
		return users.ErrUserNotFound
	}
	return nil
}

func (r *mongoRepository) AddPhoneNumber(ctx context.Context, userID string, phone string) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	// Enforce max phones limit at the DB layer: only match if the (MaxPhonesPerUser-1)th element does not exist.
	filter := bson.M{
		"_id": objID,
		fmt.Sprintf("phone_numbers.%d", users.MaxPhonesPerUser-1): bson.M{"$exists": false},
	}
	
	updateDoc := bson.M{
		"$addToSet": bson.M{"phone_numbers": phone},
		"$set": bson.M{"updated_at": time.Now()},
	}
	
	result, err := r.usersColl.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return users.ErrDuplicatePhone
		}
		log.Error().Err(err).Msg("Failed to add phone number")
		return err
	}
	if result.MatchedCount == 0 {
		// Differentiate between UserNotFound and MaxPhonesReached
		count, _ := r.usersColl.CountDocuments(ctx, bson.M{"_id": objID})
		if count == 0 {
			return users.ErrUserNotFound
		}
		return users.ErrMaxPhonesReached
	}
	return nil
}

func (r *mongoRepository) RemovePhoneNumber(ctx context.Context, userID string, phone string) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	updateDoc := bson.M{
		"$pull": bson.M{"phone_numbers": phone},
		"$set": bson.M{"updated_at": time.Now()},
	}
	
	result, err := r.usersColl.UpdateByID(ctx, objID, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to remove phone number")
		return err
	}
	if result.MatchedCount == 0 {
		return users.ErrUserNotFound
	}
	return nil
}

func (r *mongoRepository) AddWalletID(ctx context.Context, userID string, walletID string) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	filter := bson.M{
		"_id": objID,
		fmt.Sprintf("wallet_ids.%d", users.MaxWalletsPerUser-1): bson.M{"$exists": false},
	}
	
	updateDoc := bson.M{
		"$addToSet": bson.M{"wallet_ids": walletID},
		"$set": bson.M{"updated_at": time.Now()},
	}
	
	result, err := r.usersColl.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to add wallet ID")
		return err
	}
	if result.MatchedCount == 0 {
		count, _ := r.usersColl.CountDocuments(ctx, bson.M{"_id": objID})
		if count == 0 {
			return users.ErrUserNotFound
		}
		return users.ErrMaxWalletsReached
	}
	return nil
}

func (r *mongoRepository) RemoveWalletIDIfOwns(ctx context.Context, userID string, walletID string) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	filter := bson.M{
		"_id":        objID,
		"wallet_ids": walletID, // Only match if the walletID is actually in the array
	}
	
	updateDoc := bson.M{
		"$pull": bson.M{"wallet_ids": walletID},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	
	result, err := r.usersColl.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to remove wallet ID")
		return err
	}
	
	if result.ModifiedCount == 0 {
		return users.ErrWalletOwnership
	}
	return nil
}

func (r *mongoRepository) HasWallet(ctx context.Context, userID string, walletID string) (bool, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return false, users.ErrInvalidUserID
	}
	
	filter := bson.M{
		"_id":        objID,
		"wallet_ids": walletID,
	}
	
	count, err := r.usersColl.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	
	return count > 0, nil
}

// --- Refresh Tokens Collection Methods ---

func (r *mongoRepository) StoreRefreshToken(ctx context.Context, token *users.RefreshToken) error {
	log := logger.Ctx(ctx)
	result, err := r.tokensColl.InsertOne(ctx, token)
	if err != nil {
		log.Error().Err(err).Msg("Failed to store refresh token")
		return err
	}
	token.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *mongoRepository) GetRefreshToken(ctx context.Context, tokenHash string) (*users.RefreshToken, error) {
	log := logger.Ctx(ctx)
	var t users.RefreshToken
	err := r.tokensColl.FindOne(ctx, bson.M{"token_hash": tokenHash}).Decode(&t)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, users.ErrInvalidToken
		}
		log.Error().Err(err).Msg("Failed to find refresh token")
		return nil, err
	}
	return &t, nil
}

func (r *mongoRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	log := logger.Ctx(ctx)
	updateDoc := bson.M{"$set": bson.M{"revoked": true}}
	result, err := r.tokensColl.UpdateOne(ctx, bson.M{"token_hash": tokenHash}, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to revoke refresh token")
		return err
	}
	if result.MatchedCount == 0 {
		return users.ErrInvalidToken
	}
	return nil
}

func (r *mongoRepository) RevokeAllUserTokens(ctx context.Context, userID string) error {
	log := logger.Ctx(ctx)
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return users.ErrInvalidUserID
	}
	
	updateDoc := bson.M{"$set": bson.M{"revoked": true}}
	_, err = r.tokensColl.UpdateMany(ctx, bson.M{"user_id": objID}, updateDoc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to revoke all user refresh tokens")
		return err
	}
	return nil
}
