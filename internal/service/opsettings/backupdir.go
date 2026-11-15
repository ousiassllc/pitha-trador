package opsettings

// defaultBackupDirWarning is the operator hint shown while no backup
// directory is stored and the default folder on the same PC applies: that
// folder is lost together with the disk, so a destination outside this disk
// should be chosen (issue #890).
const defaultBackupDirWarning = "既定の保存先は、このアプリと同じ PC の別フォルダです。ディスクが故障すると DB と一緒に失われます。外付けドライブなどの絶対パスを指定すると、そちらに保存します。"

// WithDefaultBackupDir records dir as the backup destination that applies
// while none is stored (the folder next to the SQLite DB, issue #890). It is
// the Default shown by the Settings screen; the daily backup job resolves the
// same directory (backupjob).
func (s *Service) WithDefaultBackupDir(dir string) *Service {
	s.defaultBackupDir = dir
	return s
}
