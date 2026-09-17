package game

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

func parseVendorTransactions(encoded string) ([]sporenet.VendorTransaction, error) {
	fields := strings.Split(encoded, ";")
	transactions := make([]sporenet.VendorTransaction, 0, len(fields))
	for _, field := range fields {
		if len(field) < 2 {
			return nil, sporenet.ErrVendorOperation
		}
		operation := sporenet.VendorOperation(field[0])
		if operation != sporenet.VendorBuyback &&
			operation != sporenet.VendorPurchase &&
			operation != sporenet.VendorSell &&
			operation != sporenet.VendorFlair {
			return nil, sporenet.ErrVendorOperation
		}
		partID, err := strconv.ParseUint(field[1:], 10, 64)
		if err != nil || partID == 0 {
			return nil, sporenet.ErrVendorOperation
		}
		transactions = append(transactions, sporenet.VendorTransaction{
			Operation: operation,
			PartID:    partID,
		})
	}
	if len(transactions) == 0 {
		return nil, sporenet.ErrVendorOperation
	}
	return transactions, nil
}

func (a *API) vendorParts(
	writer http.ResponseWriter,
	request *http.Request,
	user *sporenet.User,
	encoded string,
) {
	if a.logger != nil {
		a.logger.Printf(
			"inventory_vendor_parts account=%q transactions=%q",
			userLoginName(user), encoded,
		)
	}
	if user == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	transactions, err := parseVendorTransactions(encoded)
	if err != nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	result, err := a.userManager.ApplyVendorTransactions(
		request.Context(), user, transactions, a.vendor, a.partCatalog,
	)
	if err != nil {
		if a.logger != nil && !errors.Is(err, sporenet.ErrVendorOperation) {
			a.logger.Printf(
				"inventory_vendor_parts_failed account=%q transactions=%q error=%q",
				userLoginName(user), encoded, err,
			)
		}
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	partNodes := make([]string, 0, len(result.Parts))
	// Build 103 appends response parts (sub_4C1C80); sale, buyback and flair
	// already update existing client entries optimistically (sub_435820).
	// Returning those entries creates duplicates instead of updating them.
	purchasedIDs := make(map[uint64]struct{}, len(result.PurchasedPartIDs))
	for _, partID := range result.PurchasedPartIDs {
		purchasedIDs[partID] = struct{}{}
	}
	for index := range result.Parts {
		_, isPurchased := purchasedIDs[result.Parts[index].ID]
		if !isPurchased {
			continue
		}
		partNodes = append(partNodes, partNode(&result.Parts[index]))
	}
	writeXML(writer, http.StatusOK, xmlResponse(true,
		xmlNode("parts", partNodes...),
		xmlText("dna", strconv.FormatUint(uint64(result.DNA), 10)),
	))
}
