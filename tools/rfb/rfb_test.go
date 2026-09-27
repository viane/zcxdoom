package rfb

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// testConn wires up a *Conn against one end of an in-memory pipe, with the
// handshake already done, so tests can drive Screenshot() directly against
// a fake server without a real RFB handshake or network round trip.
func testConn(t *testing.T, width, height int) (*Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })

	c := &Conn{
		c:      client,
		r:      bufio.NewReader(client),
		Width:  width,
		Height: height,
		pf: pixelFormat{
			bpp: 32, bigEndian: false, trueColor: true,
			redMax: 255, greenMax: 255, blueMax: 255,
			redShift: 16, greenShift: 8, blueShift: 0,
		},
	}
	return c, server
}

// drainFramebufferUpdateRequest reads and discards the fixed 10-byte
// FramebufferUpdateRequest that Screenshot sends before it waits for a
// response. net.Pipe is unbuffered (no internal queue between the two
// ends), so that write blocks until something reads it -- every fake
// server below must do this first, or Screenshot's own request write
// deadlocks before the server ever gets to send anything back.
func drainFramebufferUpdateRequest(server net.Conn) error {
	_, err := io.ReadFull(server, make([]byte, 10))
	return err
}

// writeFramebufferUpdate writes a FramebufferUpdate message. It returns an
// error rather than taking a *testing.T, since it's always called from a
// goroutine other than the test's own -- calling T.Fatal there would only
// mark the test as having failed a background goroutine, not the test.
func writeFramebufferUpdate(server net.Conn, rects [][]byte) error {
	hdr := []byte{0, 0, 0, 0} // type 0, padding, nRect (filled below)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(rects)))
	if _, err := server.Write(hdr); err != nil {
		return err
	}
	for _, r := range rects {
		if _, err := server.Write(r); err != nil {
			return err
		}
	}
	return nil
}

// rawRect builds one Raw-encoded rectangle: header + w*h pixels of the
// given RGB color, each encoded per testConn's little-endian 32bpp format.
func rawRect(x, y, w, h int, r, g, b uint8) []byte {
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], uint16(x))
	binary.BigEndian.PutUint16(hdr[2:4], uint16(y))
	binary.BigEndian.PutUint16(hdr[4:6], uint16(w))
	binary.BigEndian.PutUint16(hdr[6:8], uint16(h))
	binary.BigEndian.PutUint32(hdr[8:12], 0) // encoding 0: Raw
	pixel := []byte{b, g, r, 0}              // little-endian, shifts r=16 g=8 b=0
	data := make([]byte, 0, len(hdr)+w*h*4)
	data = append(data, hdr...)
	for i := 0; i < w*h; i++ {
		data = append(data, pixel...)
	}
	return data
}

func TestScreenshotSkipsBellBeforeFramebufferUpdate(t *testing.T) {
	c, server := testConn(t, 1, 1)
	serverErr := make(chan error, 1)
	go func() {
		if err := drainFramebufferUpdateRequest(server); err != nil {
			serverErr <- err
			return
		}
		if _, err := server.Write([]byte{2}); err != nil { // Bell: just the type byte
			serverErr <- err
			return
		}
		serverErr <- writeFramebufferUpdate(server, nil)
	}()

	img, err := c.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake server: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Errorf("image size = %v, want 1x1", b)
	}
}

func TestScreenshotSkipsServerCutTextBeforeFramebufferUpdate(t *testing.T) {
	c, server := testConn(t, 1, 1)
	serverErr := make(chan error, 1)
	go func() {
		if err := drainFramebufferUpdateRequest(server); err != nil {
			serverErr <- err
			return
		}
		text := []byte("clipboard contents")
		msg := make([]byte, 8+len(text))
		msg[0] = 3 // ServerCutText
		binary.BigEndian.PutUint32(msg[4:8], uint32(len(text)))
		copy(msg[8:], text)
		if _, err := server.Write(msg); err != nil {
			serverErr <- err
			return
		}
		serverErr <- writeFramebufferUpdate(server, nil)
	}()

	img, err := c.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake server: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Errorf("image size = %v, want 1x1", b)
	}
}

func TestScreenshotSkipsMultipleUnsolicitedMessages(t *testing.T) {
	c, server := testConn(t, 2, 1)
	serverErr := make(chan error, 1)
	go func() {
		if err := drainFramebufferUpdateRequest(server); err != nil {
			serverErr <- err
			return
		}
		if _, err := server.Write([]byte{2}); err != nil { // Bell
			serverErr <- err
			return
		}
		msg := make([]byte, 8) // empty ServerCutText
		msg[0] = 3
		if _, err := server.Write(msg); err != nil {
			serverErr <- err
			return
		}
		if _, err := server.Write([]byte{2}); err != nil { // Bell again
			serverErr <- err
			return
		}
		serverErr <- writeFramebufferUpdate(server, [][]byte{rawRect(0, 0, 2, 1, 255, 0, 0)})
	}()

	img, err := c.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake server: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("pixel (0,0) = (%d,%d,%d), want (255,0,0)", r>>8, g>>8, b>>8)
	}
}

func TestScreenshotDecodesRawPixels(t *testing.T) {
	c, server := testConn(t, 2, 1)
	serverErr := make(chan error, 1)
	go func() {
		if err := drainFramebufferUpdateRequest(server); err != nil {
			serverErr <- err
			return
		}
		serverErr <- writeFramebufferUpdate(server, [][]byte{rawRect(0, 0, 2, 1, 0, 0, 255)})
	}()

	img, err := c.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake server: %v", err)
	}
	r, g, b, _ := img.At(1, 0).RGBA()
	if r>>8 != 0 || g>>8 != 0 || b>>8 != 255 {
		t.Errorf("pixel (1,0) = (%d,%d,%d), want (0,0,255)", r>>8, g>>8, b>>8)
	}
}

func TestScreenshotErrorsOnUnexpectedMessageType(t *testing.T) {
	c, server := testConn(t, 1, 1)
	go func() {
		if err := drainFramebufferUpdateRequest(server); err != nil {
			return
		}
		server.Write([]byte{99})
	}()

	if _, err := c.Screenshot(); err == nil {
		t.Fatal("Screenshot succeeded on an unrecognized message type, want an error")
	}
}

func TestKeysymForKnownAndPrintable(t *testing.T) {
	if k, err := KeysymFor("Escape"); err != nil || k != 0xff1b {
		t.Errorf("KeysymFor(Escape) = %#x, %v, want 0xff1b, nil", k, err)
	}
	if k, err := KeysymFor("a"); err != nil || k != 'a' {
		t.Errorf("KeysymFor(a) = %#x, %v, want 'a', nil", k, err)
	}
	if _, err := KeysymFor("not-a-key"); err == nil {
		t.Error("KeysymFor(not-a-key) succeeded, want an error")
	}
}
