CREATE TABLE tb_mail_delivery (
  id INTEGER PRIMARY KEY AUTOINCREMENT, purpose TEXT NOT NULL CHECK(purpose IN ('verification','comment')),
  business_id INTEGER, recipient_mask TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('accepted','failed','unknown')),
  transport_mode TEXT NOT NULL CHECK(transport_mode IN ('smtp','controlled_test','disabled')),
  error_code TEXT NOT NULL DEFAULT '', create_time TEXT NOT NULL
);
CREATE INDEX idx_mail_business ON tb_mail_delivery(purpose,business_id,create_time);
