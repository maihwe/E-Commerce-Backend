package storage

import (
	"errors"
	"sync"
	"testing"

	"e-commerce-backend/database/testdb"
	"e-commerce-backend/models"
	"e-commerce-backend/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStockIsDerivedFromTheLedger checks that a product's
// stock is the sum of its movements and nothing else.
//
// This is the property the whole inventory design rests
// on: there is no stock column, so there is nothing that
// can disagree with the history that produced it.
func TestStockIsDerivedFromTheLedger(t *testing.T) {

	pool := testdb.Pool(t)

	product := newLedgerProduct(t, pool, "ledger-widget")

	// A brand new product has never moved, so it has no
	// rows at all and PostgreSQL's SUM returns NULL.
	// COALESCE turns that into zero, which is the correct
	// answer rather than an error.
	stock := currentStock(t, pool, product.ID)

	if stock != 0 {

		t.Fatalf(
			"a new product has stock %d, want 0",
			stock,
		)
	}

	movements := []struct {
		quantity int

		reason string
	}{
		{quantity: 10, reason: models.MovementRestock},

		{quantity: -3, reason: models.MovementSale},

		{quantity: 5, reason: models.MovementRestock},

		{quantity: -1, reason: models.MovementAdjustment},
	}

	expected := 0

	for _, movement := range movements {

		_, err := AddInventoryMovementInDB(
			pool,
			models.InventoryMovement{
				ProductID: product.ID,

				QuantityChange: movement.quantity,

				Reason: movement.reason,
			},
		)

		if err != nil {

			t.Fatalf(
				"could not append a movement: %v",
				err,
			)
		}

		expected += movement.quantity

		stock := currentStock(t, pool, product.ID)

		if stock != expected {

			t.Fatalf(
				"after a movement of %d the stock is %d, want %d",
				movement.quantity,
				stock,
				expected,
			)
		}
	}

	if expected != 11 {

		t.Fatalf(
			"the test itself is wrong: expected 11, got %d",
			expected,
		)
	}

	// The ledger keeps every row, including the ones that
	// took stock away. That is what makes it an audit
	// trail rather than just a counter.
	all, err := ListInventoryMovementsFromDB(
		pool,
		product.ID,
	)

	if err != nil {

		t.Fatalf(
			"could not list the ledger: %v",
			err,
		)
	}

	if len(all) != len(movements) {

		t.Fatalf(
			"the ledger has %d rows, want %d",
			len(all),
			len(movements),
		)
	}
}

// TestTwoOrdersCannotBothTakeTheLastUnit is the oversell
// test.
//
// Stock is a SUM rather than a column, so no CHECK
// constraint can stop it going negative. The guarantee
// comes from locking the product rows inside the
// confirming transaction and re-deriving the stock while
// the lock is held. This test runs two confirmations at
// the same instant to prove that the lock really does
// serialise them.
//
// Without the lock, both confirmations would read the same
// stock of one, both would decide there was enough, and
// the product would end up oversold.
func TestTwoOrdersCannotBothTakeTheLastUnit(t *testing.T) {

	pool := testdb.Pool(t)

	seller := mustUser(
		t,
		pool,
		"seller@example.com",
		models.RoleSeller,
	)

	category := mustCategory(t, pool, "Oversell", "oversell")

	product, err := CreateProductInDB(
		pool,
		models.Product{
			SellerID: seller.ID,

			CategoryID: category.ID,

			Name: "The Last One",

			Slug: "the-last-one",

			Price: 100,

			Currency: "NGN",

			IsActive: true,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not create the product: %v",
			err,
		)
	}

	// Exactly one unit exists.
	_, err = AddInventoryMovementInDB(
		pool,
		models.InventoryMovement{
			ProductID: product.ID,

			QuantityChange: 1,

			Reason: models.MovementRestock,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not restock: %v",
			err,
		)
	}

	// Two different buyers each put the last unit in
	// their cart and place an order. Both orders are
	// allowed, because creating an order checks that the
	// stock is there but does not reserve it.
	first := placeOrderFor(
		t,
		pool,
		product.ID,
		"first@example.com",
		"ECB-oversell-1",
	)

	second := placeOrderFor(
		t,
		pool,
		product.ID,
		"second@example.com",
		"ECB-oversell-2",
	)

	// Now both payments are confirmed at the same moment.
	var waitGroup sync.WaitGroup

	results := make([]error, 2)

	waitGroup.Add(2)

	go func() {

		defer waitGroup.Done()

		_, results[0] = ConfirmPaymentByReferenceInDB(
			pool,
			first.reference,
			first.amountKobo,
			"test confirmation",
		)
	}()

	go func() {

		defer waitGroup.Done()

		_, results[1] = ConfirmPaymentByReferenceInDB(
			pool,
			second.reference,
			second.amountKobo,
			"test confirmation",
		)
	}()

	waitGroup.Wait()

	succeeded := 0

	for index, err := range results {

		if err == nil {
			succeeded++
			continue
		}

		// The loser must fail for the right reason. Any
		// other error would mean the test is passing
		// for the wrong reason.
		if !errors.Is(err, ErrInsufficientStock) {

			t.Fatalf(
				"confirmation %d failed with %v, want ErrInsufficientStock",
				index,
				err,
			)
		}
	}

	if succeeded != 1 {

		t.Fatalf(
			"%d confirmations succeeded, want exactly 1",
			succeeded,
		)
	}

	// One unit was sold and none are left. A negative
	// number here would mean both confirmations took it.
	stock := currentStock(t, pool, product.ID)

	if stock != 0 {

		t.Fatalf(
			"stock is %d, want 0",
			stock,
		)
	}

	// Exactly one sale movement exists, so the ledger
	// tells the same story as the stock level.
	sales := countMovements(
		t,
		pool,
		product.ID,
		models.MovementSale,
	)

	if sales != 1 {

		t.Fatalf(
			"the ledger has %d sale movements, want 1",
			sales,
		)
	}
}

// orderUnderTest carries what a test needs to confirm a
// payment.
type orderUnderTest struct {

	orderID int

	reference string

	amountKobo int64
}

// placeOrderFor gives one buyer a cart with a product in
// it, places the order, and attaches a payment reference.
func placeOrderFor(
	t *testing.T,
	pool *pgxpool.Pool,
	productID int,
	email string,
	reference string,
) orderUnderTest {

	t.Helper()

	buyer := mustUser(t, pool, email, models.RoleBuyer)

	cartID, err := GetOrCreateCartInDB(pool, buyer.ID)

	if err != nil {

		t.Fatalf(
			"could not open a cart for %s: %v",
			email,
			err,
		)
	}

	err = AddCartItemInDB(pool, cartID, productID, 1)

	if err != nil {

		t.Fatalf(
			"could not add an item for %s: %v",
			email,
			err,
		)
	}

	order, err := CreateOrderFromCartInDB(
		pool,
		buyer.ID,
		"",
	)

	if err != nil {

		t.Fatalf(
			"could not place an order for %s: %v",
			email,
			err,
		)
	}

	order, err = SetOrderPaymentReferenceInDB(
		pool,
		order.ID,
		buyer.ID,
		reference,
	)

	if err != nil {

		t.Fatalf(
			"could not attach a reference for %s: %v",
			email,
			err,
		)
	}

	return orderUnderTest{
		orderID: order.ID,

		reference: reference,

		// Read the total back from the order rather
		// than assuming it, so a change to how totals
		// are calculated cannot silently make this
		// fixture wrong.
		amountKobo: utils.ToKobo(order.Total),
	}
}

// newLedgerProduct creates a product with no movements.
func newLedgerProduct(
	t *testing.T,
	pool *pgxpool.Pool,
	slug string,
) models.Product {

	t.Helper()

	seller := mustUser(
		t,
		pool,
		"ledger-seller@example.com",
		models.RoleSeller,
	)

	category := mustCategory(
		t,
		pool,
		"Ledger",
		"ledger",
	)

	product, err := CreateProductInDB(
		pool,
		models.Product{
			SellerID: seller.ID,

			CategoryID: category.ID,

			Name: "Ledger Widget",

			Slug: slug,

			Price: 10,

			Currency: "NGN",

			IsActive: true,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not create a product: %v",
			err,
		)
	}

	return product
}

// mustUser creates an account, or fails the test.
func mustUser(
	t *testing.T,
	pool *pgxpool.Pool,
	email string,
	role string,
) models.User {

	t.Helper()

	user, err := CreateUserInDB(
		pool,
		models.User{
			Name: email,

			Email: email,

			// These tests never sign in, so the hash
			// is a placeholder. It is not a valid
			// hash of anything, which is fine: nothing
			// here checks a password.
			PasswordHash: "not-a-real-hash",

			Role: role,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not create the user %s: %v",
			email,
			err,
		)
	}

	return user
}

// mustCategory creates a category, or fails the test.
func mustCategory(
	t *testing.T,
	pool *pgxpool.Pool,
	name string,
	slug string,
) models.Category {

	t.Helper()

	category, err := CreateCategoryInDB(
		pool,
		models.Category{Name: name, Slug: slug},
	)

	if err != nil {

		t.Fatalf(
			"could not create the category %s: %v",
			name,
			err,
		)
	}

	return category
}

// currentStock reads the derived stock level.
func currentStock(
	t *testing.T,
	pool *pgxpool.Pool,
	productID int,
) int {

	t.Helper()

	stock, err := GetProductStockFromDB(pool, productID)

	if err != nil {

		t.Fatalf(
			"could not read the stock: %v",
			err,
		)
	}

	return stock
}

// countMovements counts the movements for one product with
// one reason.
func countMovements(
	t *testing.T,
	pool *pgxpool.Pool,
	productID int,
	reason string,
) int {

	t.Helper()

	movements, err := ListInventoryMovementsFromDB(
		pool,
		productID,
	)

	if err != nil {

		t.Fatalf(
			"could not read the ledger: %v",
			err,
		)
	}

	count := 0

	for _, movement := range movements {

		if movement.Reason == reason {
			count++
		}
	}

	return count
}

// TestLedgerRefusesMeaninglessMovements checks the CHECK
// constraints that keep the ledger honest.
//
// A movement of zero is not a movement, and a reason
// outside the known list would break every query that
// groups by reason. Both are refused by PostgreSQL rather
// than by a hopeful comment in the Go code.
func TestLedgerRefusesMeaninglessMovements(t *testing.T) {

	pool := testdb.Pool(t)

	product := newLedgerProduct(
		t,
		pool,
		"constraint-widget",
	)

	cases := []struct {
		name string

		movement models.InventoryMovement
	}{
		{
			name: "a movement of zero",

			movement: models.InventoryMovement{
				ProductID:      product.ID,
				QuantityChange: 0,
				Reason:         models.MovementAdjustment,
			},
		},

		{
			name: "an invented reason",

			movement: models.InventoryMovement{
				ProductID:      product.ID,
				QuantityChange: 1,
				Reason:         "shrinkage",
			},
		},
	}

	for _, testCase := range cases {

		t.Run(testCase.name, func(t *testing.T) {

			_, err := AddInventoryMovementInDB(
				pool,
				testCase.movement,
			)

			if err == nil {

				t.Fatal(
					"the ledger accepted a movement it should have refused",
				)
			}
		})
	}
}

// TestTheSameSaleCannotBeRecordedTwice checks the
// uniqueness constraint that makes order-driven movements
// idempotent.
//
// Even if some future code path tried to confirm the same
// order twice, the database would refuse to record the
// same sale for the same product twice. It is the third of
// the three guards protecting against a double-processed
// payment.
func TestTheSameSaleCannotBeRecordedTwice(t *testing.T) {

	pool := testdb.Pool(t)

	product := newLedgerProduct(
		t,
		pool,
		"idempotent-widget",
	)

	_, err := AddInventoryMovementInDB(
		pool,
		models.InventoryMovement{
			ProductID: product.ID,

			QuantityChange: 5,

			Reason: models.MovementRestock,
		},
	)

	if err != nil {

		t.Fatalf(
			"could not restock: %v",
			err,
		)
	}

	buyer := mustUser(
		t,
		pool,
		"once@example.com",
		models.RoleBuyer,
	)

	cartID, err := GetOrCreateCartInDB(pool, buyer.ID)

	if err != nil {

		t.Fatalf("could not open a cart: %v", err)
	}

	err = AddCartItemInDB(pool, cartID, product.ID, 2)

	if err != nil {

		t.Fatalf("could not add to the cart: %v", err)
	}

	order, err := CreateOrderFromCartInDB(
		pool,
		buyer.ID,
		"",
	)

	if err != nil {

		t.Fatalf("could not place the order: %v", err)
	}

	movement := models.InventoryMovement{
		ProductID: product.ID,

		QuantityChange: -2,

		Reason: models.MovementSale,

		OrderID: &order.ID,
	}

	_, err = AddInventoryMovementInDB(pool, movement)

	if err != nil {

		t.Fatalf(
			"the first sale was refused: %v",
			err,
		)
	}

	_, err = AddInventoryMovementInDB(pool, movement)

	if err == nil {

		t.Fatal(
			"the same sale was recorded twice for one order",
		)
	}

	// The stock shows the sale happened once, not twice.
	stock := currentStock(t, pool, product.ID)

	if stock != 3 {

		t.Fatalf(
			"stock is %d, want 3",
			stock,
		)
	}
}
