// Package rfb speaks just enough of the RFB protocol (the wire protocol
// behind VNC, RFC 6143) to drive a headless server: connect, authenticate,
// grab a screenshot, and send key presses. It depends on nothing outside
// the standard library, so anything built on it compiles to one static
// binary per OS/architecture with no runtime install step.
//
// It deliberately only supports what x11vnc (as configured in this
// project's Dockerfile) actually offers: RFB 3.8, classic VNC password
// authentication, and "Raw" framebuffer encoding (the one encoding every
// RFB server is required to support, and the only one that needs no
// decompression).
package rfb

import (
	"bufio"
	"crypto/des"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"net"
	"time"
)

// Conn is an open, authenticated connection to an RFB server.
type Conn struct {
	c      net.Conn
	r      *bufio.Reader
	Width  int
	Height int
	Name   string
	pf     pixelFormat
}

type pixelFormat struct {
	bpp                             uint8
	bigEndian, trueColor            bool
	redMax, greenMax, blueMax       uint16
	redShift, greenShift, blueShift uint8
}

// Connect dials addr ("host:port"), completes the RFB handshake and VNC
// password authentication, and returns a ready-to-use connection.
func Connect(addr, password string) (*Conn, error) {
	nc, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	c := &Conn{c: nc, r: bufio.NewReader(nc)}
	if err := c.handshake(password); err != nil {
		nc.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.c.Close() }

func readN(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func (c *Conn) handshake(password string) error {
	// 1. ProtocolVersion: the server announces its version as a 12-byte
	// ASCII string, e.g. "RFB 003.008\n". We always answer with 3.8,
	// which is what every modern RFB server (x11vnc included) speaks.
	if _, err := readN(c.r, 12); err != nil {
		return fmt.Errorf("reading protocol version: %w", err)
	}
	if _, err := c.c.Write([]byte("RFB 003.008\n")); err != nil {
		return fmt.Errorf("sending protocol version: %w", err)
	}

	// 2. Security handshake: a 1-byte count, then that many 1-byte
	// security-type IDs the server offers. Type 2 is "VNC Authentication"
	// (a shared password) -- what x11vnc's -usepw enables.
	nTypesBuf, err := readN(c.r, 1)
	if err != nil {
		return fmt.Errorf("reading security type count: %w", err)
	}
	nTypes := int(nTypesBuf[0])
	if nTypes == 0 {
		reason, _ := c.readString32()
		return fmt.Errorf("server refused connection: %s", reason)
	}
	types, err := readN(c.r, nTypes)
	if err != nil {
		return fmt.Errorf("reading security types: %w", err)
	}
	found := false
	for _, t := range types {
		if t == 2 {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("server does not offer VNC password auth (offered %v)", types)
	}
	if _, err := c.c.Write([]byte{2}); err != nil {
		return fmt.Errorf("selecting VNC auth: %w", err)
	}

	// 3. VNC Authentication: the server sends a 16-byte random challenge.
	// We return it DES-encrypted with a key derived from the password.
	// VNC's one deviation from textbook DES, inherited from the original
	// AT&T implementation and still expected by every server: each key
	// byte has its bits reversed before use.
	challenge, err := readN(c.r, 16)
	if err != nil {
		return fmt.Errorf("reading auth challenge: %w", err)
	}
	response, err := vncEncrypt(password, challenge)
	if err != nil {
		return fmt.Errorf("computing auth response: %w", err)
	}
	if _, err := c.c.Write(response); err != nil {
		return fmt.Errorf("sending auth response: %w", err)
	}

	// 4. SecurityResult: 4-byte big-endian 0 (OK) or 1 (failed).
	resultBuf, err := readN(c.r, 4)
	if err != nil {
		return fmt.Errorf("reading security result: %w", err)
	}
	if binary.BigEndian.Uint32(resultBuf) != 0 {
		reason, _ := c.readString32()
		return fmt.Errorf("authentication failed: %s", reason)
	}

	// 5. ClientInit: 1 means "OK to share the desktop with other
	// viewers", which costs nothing and avoids kicking anyone off.
	if _, err := c.c.Write([]byte{1}); err != nil {
		return fmt.Errorf("sending client-init: %w", err)
	}

	// 6. ServerInit: framebuffer size, the server's native pixel format
	// (16 bytes), and a desktop name.
	initBuf, err := readN(c.r, 20)
	if err != nil {
		return fmt.Errorf("reading server-init: %w", err)
	}
	c.Width = int(binary.BigEndian.Uint16(initBuf[0:2]))
	c.Height = int(binary.BigEndian.Uint16(initBuf[2:4]))
	c.pf = pixelFormat{
		bpp:        initBuf[4],
		bigEndian:  initBuf[6] != 0,
		trueColor:  initBuf[7] != 0,
		redMax:     binary.BigEndian.Uint16(initBuf[8:10]),
		greenMax:   binary.BigEndian.Uint16(initBuf[10:12]),
		blueMax:    binary.BigEndian.Uint16(initBuf[12:14]),
		redShift:   initBuf[14],
		greenShift: initBuf[15],
		blueShift:  initBuf[16],
		// initBuf[5] is colour depth (informational) and [17:20] is padding.
	}
	nameLenBuf, err := readN(c.r, 4)
	if err != nil {
		return fmt.Errorf("reading name length: %w", err)
	}
	nameBuf, err := readN(c.r, int(binary.BigEndian.Uint32(nameLenBuf)))
	if err != nil {
		return fmt.Errorf("reading server name: %w", err)
	}
	c.Name = string(nameBuf)

	if !c.pf.trueColor {
		return fmt.Errorf("server uses a colour-mapped pixel format, which this client does not support")
	}

	// 7. SetEncodings: tell the server we only want "Raw" (type 0)
	// updates, so every pixel arrives exactly as declared above with no
	// decoding step.
	return c.setEncodings([]int32{0})
}

func (c *Conn) readString32() (string, error) {
	lenBuf, err := readN(c.r, 4)
	if err != nil {
		return "", err
	}
	s, err := readN(c.r, int(binary.BigEndian.Uint32(lenBuf)))
	return string(s), err
}

func (c *Conn) setEncodings(encodings []int32) error {
	buf := make([]byte, 4+4*len(encodings))
	buf[0] = 2 // message type: SetEncodings
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(encodings)))
	for i, e := range encodings {
		binary.BigEndian.PutUint32(buf[4+4*i:8+4*i], uint32(e))
	}
	_, err := c.c.Write(buf)
	return err
}

// vncEncrypt implements the DES half of classic VNC Authentication: the
// password becomes an 8-byte DES key (zero-padded or truncated to fit),
// each key byte's bits are reversed, and the two 8-byte halves of the
// challenge are DES-ECB-encrypted with it.
func vncEncrypt(password string, challenge []byte) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password)
	for i, b := range key {
		key[i] = reverseBits(b)
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	block.Encrypt(out[0:8], challenge[0:8])
	block.Encrypt(out[8:16], challenge[8:16])
	return out, nil
}

func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r <<= 1
		r |= b & 1
		b >>= 1
	}
	return r
}

// Screenshot requests a full (non-incremental) framebuffer update and
// decodes it into an image.Image.
//
// A real server can send other message types unprompted at any time --
// x11vnc in particular sends Bell and ServerCutText (clipboard sync)
// between clients once more than one is connected (as -shared allows).
// The read loop below skips those rather than treating them as protocol
// errors: once a client fails to skip an unexpected message correctly,
// every later read on that connection is misaligned with the byte stream
// and fails in increasingly nonsensical ways -- discovered by actually
// running two long-lived clients against a -shared server at once, which
// is exactly the scenario this project needs to support.
func (c *Conn) Screenshot() (image.Image, error) {
	req := make([]byte, 10)
	req[0] = 3 // message type: FramebufferUpdateRequest
	req[1] = 0 // incremental = false: send the whole screen, not just diffs
	binary.BigEndian.PutUint16(req[6:8], uint16(c.Width))
	binary.BigEndian.PutUint16(req[8:10], uint16(c.Height))
	if _, err := c.c.Write(req); err != nil {
		return nil, fmt.Errorf("requesting framebuffer update: %w", err)
	}

	for {
		typeBuf, err := readN(c.r, 1)
		if err != nil {
			return nil, fmt.Errorf("reading message type: %w", err)
		}
		switch typeBuf[0] {
		case 0: // FramebufferUpdate: what we asked for
			return c.readFramebufferUpdate()
		case 2: // Bell: no payload beyond the type byte
			continue
		case 3: // ServerCutText: skip its payload and keep waiting
			if err := c.skipServerCutText(); err != nil {
				return nil, err
			}
			continue
		default:
			// Including 1 (SetColourMapEntries), which the handshake's
			// true-color requirement means a well-behaved server never
			// sends us anyway.
			return nil, fmt.Errorf("unexpected message type %d while waiting for a framebuffer update", typeBuf[0])
		}
	}
}

func (c *Conn) readFramebufferUpdate() (image.Image, error) {
	hdr, err := readN(c.r, 3) // 1 padding byte + 2-byte rectangle count
	if err != nil {
		return nil, fmt.Errorf("reading update header: %w", err)
	}
	nRect := int(binary.BigEndian.Uint16(hdr[1:3]))

	img := image.NewRGBA(image.Rect(0, 0, c.Width, c.Height))
	bpp := int(c.pf.bpp) / 8

	for i := 0; i < nRect; i++ {
		rectHdr, err := readN(c.r, 12)
		if err != nil {
			return nil, fmt.Errorf("reading rectangle header: %w", err)
		}
		x := int(binary.BigEndian.Uint16(rectHdr[0:2]))
		y := int(binary.BigEndian.Uint16(rectHdr[2:4]))
		w := int(binary.BigEndian.Uint16(rectHdr[4:6]))
		h := int(binary.BigEndian.Uint16(rectHdr[6:8]))
		encoding := int32(binary.BigEndian.Uint32(rectHdr[8:12]))
		if encoding != 0 {
			return nil, fmt.Errorf("got encoding %d, only Raw (0) is supported", encoding)
		}
		data, err := readN(c.r, w*h*bpp)
		if err != nil {
			return nil, fmt.Errorf("reading raw pixel data: %w", err)
		}
		c.decodeRaw(img, x, y, w, h, data)
	}
	return img, nil
}

// skipServerCutText reads and discards a ServerCutText message's payload
// (3 padding bytes, a 4-byte length, then that many bytes of text), having
// already consumed its 1-byte message type.
func (c *Conn) skipServerCutText() error {
	lenBuf, err := readN(c.r, 7)
	if err != nil {
		return fmt.Errorf("reading ServerCutText header: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf[3:7])
	if _, err := readN(c.r, int(length)); err != nil {
		return fmt.Errorf("reading ServerCutText payload: %w", err)
	}
	return nil
}

func (c *Conn) decodeRaw(img *image.RGBA, x, y, w, h int, data []byte) {
	bpp := int(c.pf.bpp) / 8
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			off := (row*w + col) * bpp
			var raw uint32
			if c.pf.bigEndian {
				for k := 0; k < bpp; k++ {
					raw = raw<<8 | uint32(data[off+k])
				}
			} else {
				for k := bpp - 1; k >= 0; k-- {
					raw = raw<<8 | uint32(data[off+k])
				}
			}
			r := scaleColor((raw>>c.pf.redShift)&uint32(c.pf.redMax), c.pf.redMax)
			g := scaleColor((raw>>c.pf.greenShift)&uint32(c.pf.greenMax), c.pf.greenMax)
			b := scaleColor((raw>>c.pf.blueShift)&uint32(c.pf.blueMax), c.pf.blueMax)
			img.Set(x+col, y+row, color.RGBA{R: r, G: g, B: b, A: 0xff})
		}
	}
}

func scaleColor(v uint32, max uint16) uint8 {
	if max == 0 {
		return 0
	}
	return uint8(v * 255 / uint32(max))
}

// SendKey sends a single key-down (down=true) or key-up event for the
// given X11 keysym.
func (c *Conn) SendKey(keysym uint32, down bool) error {
	buf := make([]byte, 8)
	buf[0] = 4 // message type: KeyEvent
	if down {
		buf[1] = 1
	}
	binary.BigEndian.PutUint32(buf[4:8], keysym)
	_, err := c.c.Write(buf)
	return err
}

// Tap presses and releases a key, holding it just long enough for DOOM's
// input polling to reliably notice it.
func (c *Conn) Tap(keysym uint32) error {
	if err := c.SendKey(keysym, true); err != nil {
		return err
	}
	time.Sleep(80 * time.Millisecond)
	return c.SendKey(keysym, false)
}
