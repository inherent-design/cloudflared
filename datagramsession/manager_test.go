package datagramsession

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/packet"
)

var (
	nopLogger = zerolog.Nop()
)

func TestManagerServe(t *testing.T) {
	const (
		sessions            = 2
		msgs                = 5
		remoteUnregisterMsg = "eyeball closed connection"
	)

	requestChan := make(chan *packet.Session)
	transport := mockQUICTransport{
		sessions: make(map[uuid.UUID]chan []byte),
	}
	for i := 0; i < sessions; i++ {
		transport.sessions[uuid.New()] = make(chan []byte)
	}

	mg := NewManager(&nopLogger, transport.MuxSession, requestChan)

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func(ctx context.Context) {
		serveErr <- mg.Serve(ctx)
	}(ctx)

	errGroup, ctx := errgroup.WithContext(ctx)
	for sessionID, eyeballRespChan := range transport.sessions {
		// Assign loop variables to local variables
		sID := sessionID
		payload := testPayload(sID)
		expectResp := testResponse(payload)

		cfdConn, originConn := net.Pipe()

		origin := mockOrigin{
			expectMsgCount: msgs,
			expectedMsg:    payload,
			expectedResp:   expectResp,
			conn:           originConn,
		}

		eyeball := mockEyeballSession{
			id:               sID,
			expectedMsgCount: msgs,
			expectedMsg:      payload,
			expectedResponse: expectResp,
			respReceiver:     eyeballRespChan,
		}

		// Assign loop variables to local variables
		errGroup.Go(func() error {
			session, err := mg.RegisterSession(ctx, sID, cfdConn)
			require.NoError(t, err)
			reqErrGroup, reqCtx := errgroup.WithContext(ctx)
			reqErrGroup.Go(func() error {
				return origin.serve()
			})
			reqErrGroup.Go(func() error {
				return eyeball.serve(reqCtx, requestChan)
			})

			sessionDone := make(chan struct {
				closedByRemote bool
				err            error
			}, 1)
			go func() {
				closedByRemote, err := session.Serve(ctx, time.Minute*2)
				sessionDone <- struct {
					closedByRemote bool
					err            error
				}{
					closedByRemote: closedByRemote,
					err:            err,
				}
			}()

			// Make sure eyeball and origin have received all messages before unregistering the session
			require.NoError(t, reqErrGroup.Wait())

			require.NoError(t, mg.UnregisterSession(ctx, sID, remoteUnregisterMsg, true))
			result := <-sessionDone
			require.Equal(t, &errClosedSession{
				message:  remoteUnregisterMsg,
				byRemote: true,
			}, result.err)
			require.True(t, result.closedByRemote)
			return nil
		})
	}

	require.NoError(t, errGroup.Wait())
	cancel()
	require.ErrorIs(t, <-serveErr, context.Canceled)
}

func TestTimeout(t *testing.T) {
	const (
		testTimeout = time.Millisecond * 50
	)

	mg := NewManager(&nopLogger, nil, nil)
	mg.timeout = testTimeout
	ctx := context.Background()
	sessionID := uuid.New()
	// session manager is not running, so event loop is not running and therefore calling the APIs should timeout
	session, err := mg.RegisterSession(ctx, sessionID, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, session)

	err = mg.UnregisterSession(ctx, sessionID, "session gone", true)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestUnregisterSessionCloseSession(t *testing.T) {
	sessionID := uuid.New()
	payload := []byte(t.Name())
	sender := newMockTransportSender(sessionID, payload)
	mg := NewManager(&nopLogger, sender.muxSession, nil)
	ctx, cancel := context.WithCancel(context.Background())

	managerDone := make(chan error, 1)
	go func() {
		managerDone <- mg.Serve(ctx)
	}()

	cfdConn, originConn := net.Pipe()
	session, err := mg.RegisterSession(ctx, sessionID, cfdConn)
	require.NoError(t, err)
	require.NotNil(t, session)

	unregisteredChan := make(chan error, 1)
	go func() {
		_, err := originConn.Write(payload)
		if err != nil {
			unregisteredChan <- err
			return
		}

		err = mg.UnregisterSession(ctx, sessionID, "eyeball closed session", true)
		unregisteredChan <- err
	}()

	closedByRemote, err := session.Serve(ctx, time.Minute)
	require.True(t, closedByRemote)
	require.Error(t, err)

	require.NoError(t, <-unregisteredChan)
	cancel()
	require.ErrorIs(t, <-managerDone, context.Canceled)
}

func TestManagerShutdownClosesSessionsAsRemote(t *testing.T) {
	sessionID := uuid.New()
	mg := NewManager(&nopLogger, nil, nil)
	managerCtx, cancelManager := context.WithCancel(t.Context())
	sessionCtx := t.Context()

	managerDone := make(chan error, 1)
	go func() {
		managerDone <- mg.Serve(managerCtx)
	}()

	cfdConn, originConn := net.Pipe()
	defer func() { require.NoError(t, originConn.Close()) }()
	session, err := mg.RegisterSession(sessionCtx, sessionID, cfdConn)
	require.NoError(t, err)
	require.NotNil(t, session)

	type sessionResult struct {
		closedByRemote bool
		err            error
	}
	results := make(chan sessionResult, 1)
	go func() {
		closedByRemote, err := session.Serve(sessionCtx, time.Minute)
		results <- sessionResult{
			closedByRemote: closedByRemote,
			err:            err,
		}
	}()

	cancelManager()
	result := <-results
	require.True(t, result.closedByRemote)
	require.Equal(t, &errClosedSession{
		message:  context.Canceled.Error(),
		byRemote: true,
	}, result.err)

	require.ErrorIs(t, <-managerDone, context.Canceled)
}

type mockOrigin struct {
	expectMsgCount int
	expectedMsg    []byte
	expectedResp   []byte
	conn           io.ReadWriteCloser
}

func (mo *mockOrigin) serve() error {
	expectedMsgLen := len(mo.expectedMsg)
	readBuffer := make([]byte, expectedMsgLen+1)
	for i := 0; i < mo.expectMsgCount; i++ {
		n, err := mo.conn.Read(readBuffer)
		if err != nil {
			return err
		}
		if n != expectedMsgLen {
			return fmt.Errorf("Expect to read %d bytes, read %d", expectedMsgLen, n)
		}
		if !bytes.Equal(readBuffer[:n], mo.expectedMsg) {
			return fmt.Errorf("Expect %v, read %v", mo.expectedMsg, readBuffer[:n])
		}
		_, err = mo.conn.Write(mo.expectedResp)
		if err != nil {
			return err
		}
	}
	return nil
}

func testPayload(sessionID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("Message from %s", sessionID))
}

func testResponse(msg []byte) []byte {
	return []byte(fmt.Sprintf("Response to %v", msg))
}

type mockQUICTransport struct {
	sessions map[uuid.UUID]chan []byte
}

func (me *mockQUICTransport) MuxSession(session *packet.Session) error {
	s := me.sessions[session.ID]
	s <- session.Payload
	return nil
}

type mockEyeballSession struct {
	id               uuid.UUID
	expectedMsgCount int
	expectedMsg      []byte
	expectedResponse []byte
	respReceiver     <-chan []byte
}

func (me *mockEyeballSession) serve(ctx context.Context, requestChan chan *packet.Session) error {
	for i := 0; i < me.expectedMsgCount; i++ {
		requestChan <- &packet.Session{
			ID:      me.id,
			Payload: me.expectedMsg,
		}
		resp := <-me.respReceiver
		if !bytes.Equal(resp, me.expectedResponse) {
			return fmt.Errorf("Expect %v, read %v", me.expectedResponse, resp)
		}
	}
	return nil
}
