CREATE TABLE tb_photo (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  album_id INTEGER NOT NULL,
  photo_name TEXT NOT NULL,
  photo_desc TEXT NULL DEFAULT NULL,
  photo_src TEXT NOT NULL,
  is_delete INTEGER NOT NULL DEFAULT 0,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  FOREIGN KEY (album_id) REFERENCES tb_photo_album(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX idx_photo_album ON tb_photo(album_id,is_delete,id);

CREATE TABLE tb_photo_album (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  album_name TEXT NOT NULL,
  album_desc TEXT NOT NULL,
  album_cover TEXT NOT NULL,
  is_delete INTEGER NOT NULL DEFAULT 0,
  status INTEGER NOT NULL DEFAULT 1,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL
);
CREATE INDEX idx_album_public ON tb_photo_album(status,is_delete,id);

CREATE TABLE tb_talk (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  content TEXT NOT NULL,
  images TEXT NULL DEFAULT NULL,
  is_top INTEGER NOT NULL DEFAULT 0,
  status INTEGER NOT NULL DEFAULT 1,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  FOREIGN KEY (user_id) REFERENCES tb_user_info(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX idx_talk_public ON tb_talk(status,is_top DESC,create_time DESC,id DESC);
