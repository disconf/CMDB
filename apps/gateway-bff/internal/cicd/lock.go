package cicd

import "context"

func (s *Service) WithAdvisoryLock(ctx context.Context, key int64, fn func() error) (bool, error) {
	if s.db == nil {
		return true, fn()
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
	return true, fn()
}
