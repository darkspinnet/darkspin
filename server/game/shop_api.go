package game

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type PartShopPreparer interface {
	PreparePartShop(context.Context, *sporenet.User, sporenet.PartShopCatalog,
		*sporenet.Vendor, sporenet.PartShopRules, time.Time) (sporenet.PartShopStock, error)
}

type BuybackExpirer interface {
	ExpireBuyback(context.Context, *sporenet.User, time.Time) error
}

func (e *API) partOffers(writer http.ResponseWriter, request *http.Request, user *sporenet.User) {
	if user == nil || e.partShopPreparer == nil {
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	stock, err := e.partShopPreparer.PreparePartShop(request.Context(), user,
		e.partCatalog, e.vendor, e.partShopRules, time.Now())
	if err != nil {
		e.logger.Printf("part_shop_list_failed account=%q error=%q", userLoginName(user), err)
		writeXML(writer, http.StatusOK, xmlResponse(false))
		return
	}
	nodes := make([]string, 0, len(stock.Parts))
	for index := range stock.Parts {
		nodes = append(nodes, partNode(&stock.Parts[index]))
	}
	writeXML(writer, http.StatusOK, xmlResponse(true, xmlNode("parts", nodes...),
		xmlText("itemstore_offer_period", strconv.FormatUint(uint64(e.partShopRules.RefreshSecond), 10)),
		xmlText("itemstore_current_expiration", strconv.FormatInt(stock.ExpiresAt, 10)),
		xmlText("item_level", strconv.FormatUint(uint64(stock.ItemLevel), 10))))
}
