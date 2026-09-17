package sporenet

import (
	"fmt"
	"sync"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
)

// RoomView groups related room categories.
type RoomView struct {
	ID   uint32
	Name string
}

func (v *RoomView) Fields() []tdf.Field {
	return v.FieldsWithDisplay("hello")
}

// FieldsWithDisplay encodes the view with a caller-owned transient display
// message. Blaze session handlers use this to avoid replaying the same message
// on every scene-driven view refresh.
func (v *RoomView) FieldsWithDisplay(display string) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("DISP", tdf.StringValue(display)),
		tdf.FieldNamed("GMET", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("META", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("MXRM", tdf.IntegerValue(1)),
		tdf.FieldNamed("NAME", tdf.StringValue(v.Name)),
		tdf.FieldNamed("USRM", tdf.IntegerValue(0)),
		tdf.FieldNamed("VWID", tdf.IntegerValue(uint64(v.ID))),
	}
}

// RoomCategory describes one lobby category.
type RoomCategory struct {
	ID          uint32
	ViewID      uint32
	Name        string
	Description string
	Password    string
}

func (c *RoomCategory) Fields() []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("CAPA", tdf.IntegerValue(10)),
		tdf.FieldNamed("CMET", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("CRIT", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("CTID", tdf.IntegerValue(uint64(c.ID))),
		tdf.FieldNamed("DESC", tdf.StringValue(c.Description)),
		tdf.FieldNamed("DISP", tdf.StringValue("")),
		tdf.FieldNamed("DISR", tdf.StringValue("")),
		tdf.FieldNamed("EMAX", tdf.IntegerValue(10)),
		tdf.FieldNamed("EPCT", tdf.IntegerValue(0)),
		tdf.FieldNamed("FLAG", tdf.IntegerValue(0)),
		tdf.FieldNamed("GMET", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("LOCL", tdf.StringValue("")),
		tdf.FieldNamed("NAME", tdf.StringValue(c.Name)),
		tdf.FieldNamed("NEXP", tdf.IntegerValue(1)),
		tdf.FieldNamed("PASS", tdf.StringValue(c.Password)),
		tdf.FieldNamed("UCRT", tdf.IntegerValue(1)),
		tdf.FieldNamed("VWID", tdf.IntegerValue(uint64(c.ViewID))),
	}
}

// Room is a lobby and its current users.
type Room struct {
	mu sync.RWMutex

	ID         uint32
	CategoryID uint32
	Name       string
	Password   string
	Capacity   uint32
	users      map[int64]*User
}

// RoomManager owns static and user-created lobby objects.
type RoomManager struct {
	mu         sync.RWMutex
	rooms      map[uint32]*Room
	categories map[uint32]*RoomCategory
	views      map[uint32]*RoomView
}

// NewRoomManager creates the four static lobbies used by recap_server.
func NewRoomManager() *RoomManager {
	manager := &RoomManager{
		rooms: make(map[uint32]*Room), categories: make(map[uint32]*RoomCategory), views: make(map[uint32]*RoomView),
	}
	manager.views[1] = &RoomView{ID: 1, Name: "Lobby View #1"}
	for id := uint32(1); id <= 4; id++ {
		manager.categories[id] = &RoomCategory{ID: id, ViewID: 1, Name: fmt.Sprintf("Lobby Category #%d", id)}
		manager.rooms[id] = &Room{ID: id, CategoryID: id, Name: fmt.Sprintf("Lobby #%d", id), Capacity: 10, users: make(map[int64]*User)}
	}
	return manager
}

func (m *RoomManager) Room(id uint32) *Room {
	m.mu.RLock()
	room := m.rooms[id]
	m.mu.RUnlock()
	return room
}

func (m *RoomManager) Category(id uint32) *RoomCategory {
	m.mu.RLock()
	category := m.categories[id]
	m.mu.RUnlock()
	return category
}

func (m *RoomManager) View(id uint32) *RoomView {
	m.mu.RLock()
	view := m.views[id]
	m.mu.RUnlock()
	return view
}

func (m *RoomManager) CreateRoom() *Room {
	m.mu.Lock()
	id := nextMapID(m.rooms)
	room := &Room{ID: id, Name: fmt.Sprintf("Lobby #%d", id), Capacity: 10, users: make(map[int64]*User)}
	m.rooms[id] = room
	m.mu.Unlock()
	return room
}

func (m *RoomManager) CreateCategory() *RoomCategory {
	m.mu.Lock()
	id := nextMapID(m.categories)
	category := &RoomCategory{ID: id, Name: fmt.Sprintf("Lobby Category #%d", id)}
	m.categories[id] = category
	m.mu.Unlock()
	return category
}

func (m *RoomManager) CreateView() *RoomView {
	m.mu.Lock()
	id := nextMapID(m.views)
	view := &RoomView{ID: id, Name: fmt.Sprintf("Lobby View #%d", id)}
	m.views[id] = view
	m.mu.Unlock()
	return view
}

// SetCategory moves the room into a category.
func (r *Room) SetCategory(category *RoomCategory) {
	r.mu.Lock()
	if category == nil {
		r.CategoryID = 0
	} else {
		r.CategoryID = category.ID
	}
	r.mu.Unlock()
}

func (m *RoomManager) RemoveRoom(id uint32) {
	m.mu.Lock()
	delete(m.rooms, id)
	m.mu.Unlock()
}

// AddUser joins a user and updates both sides of the relationship.
func (r *Room) AddUser(user *User) bool {
	if user == nil {
		return false
	}
	r.mu.Lock()
	if uint32(len(r.users)) >= r.Capacity {
		r.mu.Unlock()
		return false
	}
	r.users[user.Account.ID] = user
	r.mu.Unlock()
	user.mu.Lock()
	user.RoomID = r.ID
	user.mu.Unlock()
	return true
}

// RemoveUser leaves a room.
func (r *Room) RemoveUser(user *User) {
	if user == nil {
		return
	}
	r.mu.Lock()
	delete(r.users, user.Account.ID)
	r.mu.Unlock()
	user.mu.Lock()
	if user.RoomID == r.ID {
		user.RoomID = 0
	}
	user.mu.Unlock()
}

// Users returns a snapshot of the lobby's current members.
func (r *Room) Users() []*User {
	r.mu.RLock()
	users := make([]*User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, user)
	}
	r.mu.RUnlock()
	return users
}

// Fields returns the RoomData TDF structure contents.
func (r *Room) Fields(category *RoomCategory) []tdf.Field {
	r.mu.RLock()
	userIDs := make([]tdf.Value, 0, len(r.users))
	for id := range r.users {
		userIDs = append(userIDs, tdf.IntegerValue(uint64(id)))
	}
	population := len(r.users)
	r.mu.RUnlock()
	categoryName := "Unknown category"
	categoryID := uint32(0)
	if category != nil {
		categoryName = category.Name
		categoryID = category.ID
	}
	return []tdf.Field{
		tdf.FieldNamed("AREM", tdf.IntegerValue(1)),
		tdf.FieldNamed("ATTR", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("BLST", tdf.ListValue(tdf.Integer, userIDs...)),
		tdf.FieldNamed("CAP", tdf.IntegerValue(uint64(r.Capacity))),
		tdf.FieldNamed("CNAM", tdf.StringValue(categoryName)),
		tdf.FieldNamed("CRET", tdf.IntegerValue(0)),
		tdf.FieldNamed("CRIT", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("CRTM", tdf.IntegerValue(0)),
		tdf.FieldNamed("CTID", tdf.IntegerValue(uint64(categoryID))),
		tdf.FieldNamed("ENUM", tdf.IntegerValue(1)),
		tdf.FieldNamed("HNAM", tdf.StringValue("Lobby")),
		tdf.FieldNamed("HOST", tdf.IntegerValue(1)),
		tdf.FieldNamed("NAME", tdf.StringValue(r.Name)),
		tdf.FieldNamed("POPU", tdf.IntegerValue(uint64(population))),
		tdf.FieldNamed("PSWD", tdf.StringValue(r.Password)),
		tdf.FieldNamed("PVAL", tdf.StringValue("")),
		tdf.FieldNamed("RMID", tdf.IntegerValue(uint64(r.ID))),
		tdf.FieldNamed("UCRT", tdf.IntegerValue(1)),
	}
}

func nextMapID[T any](values map[uint32]T) uint32 {
	var highest uint32
	for id := range values {
		if id > highest {
			highest = id
		}
	}
	return highest + 1
}
