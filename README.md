# Spotogether

Theo dõi những bài nhạc đang phát ở The Coffee House rồi cố tìm và play nó trên Spotify.

## Cài đặt

```
go get github.com/nguyenvanduocit/tchmusic
```
## Config

Tạo ứng dụng trên Spotify, và đặt redirect URL là:

```
http://127.0.0.1:8090/callback
```

Tạo file `.tchmusic.yaml` ở thư mục home (`~`):

```yaml
client_id:
secret_key:
```

## Sử dụng

Mở Spotify lên và chọn một device, Trên PC thì có thể bấn play một bài hát nào đó rồi pause nó lại, khi đó Spotify sẽ set PC làm default device.

Chạy lệnh:

```
tchmusic
```

Browser sẽ mở lên yêu cầu bạn login. Sau khi login trang sẽ tự đóng. Giờ nếu Spotify đang play thì tchmusic sẽ đợi đên khi play xong sẽ kiểm tra bài mới. Nếu bài mới không tồn tại trên Spotify thì sẽ gợi ý cho bạn một bài hát khác dựa trên 4 Genres của top artist của bạn.
