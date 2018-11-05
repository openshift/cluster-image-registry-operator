package audit

import (
	"context"
	"sync"

	dcontext "github.com/docker/distribution/context"
	"github.com/sirupsen/logrus"
)

const (
	LogEntryType     = "openshift.logger"
	AuditUserEntry   = "openshift.auth.user"
	AuditUserIDEntry = "openshift.auth.userid"
	AuditStatusEntry = "openshift.request.status"
	AuditErrorEntry  = "openshift.request.error"

	auditLoggerKey = "openshift.audit.logger"

	DefaultLoggerType = "registry"
	AuditLoggerType   = "audit"

	OpStatusBegin = "begin"
	OpStatusError = "error"
	OpStatusOK    = "success"
)

// Logger implements special audit log. We can't use the system logger because
// the change of log level can hide the audit logs.
type Logger struct {
	mu     sync.Mutex
	ctx    context.Context
	logger *logrus.Logger
}

// NewLogger returns new audit logger which inherits fields from the system logger.
func NewLogger(ctx context.Context) *Logger {
	logger := &Logger{
		logger: logrus.New(),
		ctx:    ctx,
	}
	if entry, ok := dcontext.GetLogger(ctx).(*logrus.Entry); ok {
		logger.SetFormatter(entry.Logger.Formatter)
	} else if lgr, ok := dcontext.GetLogger(ctx).(*logrus.Logger); ok {
		logger.SetFormatter(lgr.Formatter)
	}
	return logger
}

// SetFormatter sets the audit logger formatter.
func (l *Logger) SetFormatter(formatter logrus.Formatter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Formatter = formatter
}

// Log logs record.
func (l *Logger) Log(args ...interface{}) {
	auditFields := logrus.Fields{
		LogEntryType:     AuditLoggerType,
		AuditStatusEntry: OpStatusBegin,
	}
	l.getEntry().WithFields(auditFields).Info(args...)
}

// Logf formats record according to a format.
func (l *Logger) Logf(format string, args ...interface{}) {
	auditFields := logrus.Fields{
		LogEntryType: AuditLoggerType,
	}
	l.getEntry().WithFields(auditFields).Infof(format, args...)
}

// LogResult logs record with additional operation status.
func (l *Logger) LogResult(err error, args ...interface{}) {
	auditFields := logrus.Fields{
		LogEntryType:     AuditLoggerType,
		AuditStatusEntry: OpStatusOK,
	}
	if err != nil {
		auditFields[AuditErrorEntry] = err
		auditFields[AuditStatusEntry] = OpStatusError
	}
	l.getEntry().WithFields(auditFields).Info(args...)
}

// LogResultf formats record according to a format with additional operation status.
func (l *Logger) LogResultf(err error, format string, args ...interface{}) {
	auditFields := logrus.Fields{
		LogEntryType:     AuditLoggerType,
		AuditStatusEntry: OpStatusOK,
	}
	if err != nil {
		auditFields[AuditErrorEntry] = err
		auditFields[AuditStatusEntry] = OpStatusError
	}
	l.getEntry().WithFields(auditFields).Infof(format, args...)
}

func (l *Logger) getEntry() *logrus.Entry {
	if entry, ok := dcontext.GetLogger(l.ctx).(*logrus.Entry); ok {
		return l.logger.WithFields(entry.Data)
	}
	return logrus.NewEntry(l.logger)
}

// LoggerExists checks audit logger existence.
func LoggerExists(ctx context.Context) (exists bool) {
	_, exists = ctx.Value(auditLoggerKey).(*Logger)
	return
}

// GetLogger returns the logger from the current context, if present. It will be created otherwise.
func GetLogger(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(auditLoggerKey).(*Logger); ok {
		return logger
	}
	return NewLogger(ctx)
}

// WithLogger creates a new context with provided logger.
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, auditLoggerKey, logger)
}
