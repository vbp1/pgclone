package rsync

// Add returns element-wise sum of two Stats.
func (s Stats) Add(o Stats) Stats {
	return Stats{
		NumFiles:             s.NumFiles + o.NumFiles,
		CreatedFiles:         s.CreatedFiles + o.CreatedFiles,
		DeletedFiles:         s.DeletedFiles + o.DeletedFiles,
		RegTransferred:       s.RegTransferred + o.RegTransferred,
		TotalFileSize:        s.TotalFileSize + o.TotalFileSize,
		TotalTransferredSize: s.TotalTransferredSize + o.TotalTransferredSize,
		LiteralData:          s.LiteralData + o.LiteralData,
		MatchedData:          s.MatchedData + o.MatchedData,
		RegFiles:             s.RegFiles + o.RegFiles,
		DirFiles:             s.DirFiles + o.DirFiles,
		LinkFiles:            s.LinkFiles + o.LinkFiles,
		FileListSize:         s.FileListSize + o.FileListSize,
		FileListGenSeconds:   maxFloat(s.FileListGenSeconds, o.FileListGenSeconds),
		BytesSent:            s.BytesSent + o.BytesSent,
		BytesReceived:        s.BytesReceived + o.BytesReceived,
		CreatedReg:           s.CreatedReg + o.CreatedReg,
		CreatedDir:           s.CreatedDir + o.CreatedDir,
		DeletedReg:           s.DeletedReg + o.DeletedReg,
		DeletedDir:           s.DeletedDir + o.DeletedDir,
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
