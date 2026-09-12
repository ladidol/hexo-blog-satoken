CREATE TABLE tb_operation_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  opt_module TEXT NOT NULL,
  opt_type TEXT NOT NULL,
  opt_url TEXT NOT NULL,
  opt_method TEXT NOT NULL,
  opt_desc TEXT NOT NULL,
  request_param TEXT NOT NULL,
  request_method TEXT NOT NULL,
  response_data TEXT NOT NULL,
  user_id INTEGER NOT NULL,
  nickname TEXT NOT NULL,
  ip_address TEXT NOT NULL,
  ip_source TEXT NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);
CREATE INDEX idx_log_time ON tb_operation_log(create_time DESC,id DESC);

CREATE TABLE tb_unique_view (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  views_count INTEGER NOT NULL,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);
CREATE UNIQUE INDEX idx_uv_day ON tb_unique_view(substr(create_time,1,10));

CREATE TABLE tb_visit_record (
  visit_date TEXT NOT NULL, visitor_key TEXT NOT NULL,
  ip_source TEXT NOT NULL DEFAULT '', create_time TEXT NOT NULL,
  PRIMARY KEY(visit_date,visitor_key)
);
