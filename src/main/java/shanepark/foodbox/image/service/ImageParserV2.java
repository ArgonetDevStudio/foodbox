package shanepark.foodbox.image.service;

import lombok.RequiredArgsConstructor;
import org.json.JSONArray;
import org.json.JSONObject;
import org.springframework.stereotype.Component;
import shanepark.foodbox.api.exception.ImageParseException;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.ocr.OCRConfig;

import java.awt.image.BufferedImage;
import java.io.*;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.file.Files;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;

@Component
@RequiredArgsConstructor
public class ImageParserV2 {

    public List<ParsedMenu> parse(InputStream inputStream, OCRConfig ocrConfig) {
        String apiUrl = ocrConfig.getApiUrl();
        String secretKey = ocrConfig.getSecretKey();
        try {
            File imageFile = File.createTempFile(String.valueOf(inputStream.hashCode()), ".png");
            imageFile.deleteOnExit();
            copyInputStreamToFile(inputStream, imageFile);

            List<ParsedMenu> days = new ArrayList<>();

            // Create JSON request body
            JSONObject requestJson = new JSONObject();
            JSONArray images = new JSONArray();
            JSONObject imageInfo = new JSONObject();
            imageInfo.put("format", "png");
            imageInfo.put("name", "demo");
            images.put(imageInfo);
            requestJson.put("images", images);
            requestJson.put("requestId", UUID.randomUUID().toString());
            requestJson.put("version", "V2");
            requestJson.put("timestamp", System.currentTimeMillis());

            // Create payload
            String message = requestJson.toString();
            String boundary = "----WebKitFormBoundary" + UUID.randomUUID().toString();
//            File imageFile = new File(imageFilePath);

            HttpURLConnection connection = (HttpURLConnection) new URL(apiUrl).openConnection();
            connection.setRequestMethod("POST");
            connection.setDoOutput(true);
            connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);
            connection.setRequestProperty("X-OCR-SECRET", secretKey);

            try (OutputStream outputStream = connection.getOutputStream();
                 PrintWriter writer = new PrintWriter(new OutputStreamWriter(outputStream, "UTF-8"), true)) {

                // Add JSON payload
                writer.append("--").append(boundary).append("\r\n");
                writer.append("Content-Disposition: form-data; name=\"message\"\r\n");
                writer.append("Content-Type: application/json; charset=UTF-8\r\n\r\n");
                writer.append(message).append("\r\n");

                // Add image file
                writer.append("--").append(boundary).append("\r\n");
                writer.append("Content-Disposition: form-data; name=\"file\"; filename=\"").append(imageFile.getName()).append("\"\r\n");
                writer.append("Content-Type: ").append(Files.probeContentType(imageFile.toPath())).append("\r\n\r\n");
                writer.flush();

                Files.copy(imageFile.toPath(), outputStream);
                outputStream.flush();
                writer.append("\r\n");
                writer.append("--").append(boundary).append("--").append("\r\n");
                writer.flush();
            }

            // Get response

            int responseCode = connection.getResponseCode();
            if (responseCode == HttpURLConnection.HTTP_OK) {
                try (BufferedReader reader = new BufferedReader(new InputStreamReader(connection.getInputStream(), "UTF-8"))) {
                    StringBuilder response = new StringBuilder();
                    String line;
                    while ((line = reader.readLine()) != null) {
                        response.append(line);
                    }

                    // Parse and print the response
                    JSONObject jsonResponse = new JSONObject(response.toString());
                    JSONArray fields = jsonResponse.getJSONArray("images").getJSONObject(0).getJSONArray("fields");


                    String date1 = "";
                    String date2 = "";
                    String menu1 = "";
                    String menu2 = "";
                    String menu3 = "";
                    String menu4 = "";
                    String menu5 = "";
                    String menu6 = "";
                    String menu7 = "";
                    String menu8 = "";
                    String menu9 = "";
                    String menu10 = "";
                    for (int i = 0; i < fields.length(); i++) {
                        JSONObject obj = (JSONObject) fields.get(i);
                        String val = (String) obj.getString("name");
                        if("날짜1".equals(val)){
                            date1 = obj.getString("inferText");
                            continue;
                        }
                        if("날짜2".equals(val)){
                            date2 = obj.getString("inferText");
                            continue;
                        }
                        switch(val) {
                            case "날짜1":
                                date1 = obj.getString("inferText");
                                break;
                            case "날짜2":
                                date2 = obj.getString("inferText");
                                break;
                            case "메뉴1":
                                menu1 = obj.getString("inferText");
                                break;
                            case "메뉴2":
                                menu2 = obj.getString("inferText");
                                break;
                            case "메뉴3":
                                menu3 = obj.getString("inferText");
                                break;
                            case "메뉴4":
                                menu4 = obj.getString("inferText");
                                break;
                            case "메뉴5":
                                menu5 = obj.getString("inferText");
                                break;
                            case "메뉴6":
                                menu6 = obj.getString("inferText");
                                break;
                            case "메뉴7":
                                menu7 = obj.getString("inferText");
                                break;
                            case "메뉴8":
                                menu8 = obj.getString("inferText");
                                break;
                            case "메뉴9":
                                menu9 = obj.getString("inferText");
                                break;
                            case "메뉴10":
                                menu10 = obj.getString("inferText");
                                break;
                            default:
                                throw new RuntimeException("해당 타입 없음.");
                        }
                    }
                    days = setDates(date1,date2);
                    days.get(0).setMenu(menu1);
                    days.get(1).setMenu(menu2);
                    days.get(2).setMenu(menu3);
                    days.get(3).setMenu(menu4);
                    days.get(4).setMenu(menu5);
                    days.get(5).setMenu(menu6);
                    days.get(6).setMenu(menu7);
                    days.get(7).setMenu(menu8);
                    days.get(8).setMenu(menu9);
                    days.get(9).setMenu(menu10);
                    System.out.println(days);
                }
            } else {
                System.out.println("Error: " + responseCode);
            }
            return days;
        } catch (Exception e) {
            throw new ImageParseException(e);
        }

    }

    private static void copyInputStreamToFile(InputStream inputStream, File file) {

        try (FileOutputStream outputStream = new FileOutputStream(file)) {
            int read;
            byte[] bytes = new byte[1024];

            while ((read = inputStream.read(bytes)) != -1) {
                outputStream.write(bytes, 0, read);
            }
        } catch (FileNotFoundException e) {
            throw new RuntimeException(e);
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }

    private List<ParsedMenu> setDates(String date1, String date2) {
        String[] arr1 = date1.split(" ");
        String[] arr2 = date1.split(" ");
        List<String> list1 = new ArrayList<>(Arrays.asList(arr1));
        List<String> list2 = new ArrayList<>(Arrays.asList(arr2));
        list1.addAll(list2);
        List<ParsedMenu> days = new ArrayList<>();
        for (int i = 0; i < list1.size(); i++) {
            days.add(new ParsedMenu(list1.get(i)));
        }
        return days;
    }

    public static JSONObject convertBufferedImageToJson(BufferedImage bufferedImage) {
        int width = bufferedImage.getWidth();
        int height = bufferedImage.getHeight();

        // JSON 객체 생성
        JSONObject jsonObject = new JSONObject();
        jsonObject.put("width", width);
        jsonObject.put("height", height);

        JSONArray pixelArray = new JSONArray();

        // 이미지 픽셀 데이터를 JSON 배열로 변환
        for (int y = 0; y < height; y++) {
            JSONArray row = new JSONArray();
            for (int x = 0; x < width; x++) {
                int pixel = bufferedImage.getRGB(x, y);

                // ARGB 값 추출
                int alpha = (pixel >> 24) & 0xff;
                int red = (pixel >> 16) & 0xff;
                int green = (pixel >> 8) & 0xff;
                int blue = pixel & 0xff;

                // JSON 객체로 픽셀 정보 추가
                JSONObject pixelJson = new JSONObject();
                pixelJson.put("alpha", alpha);
                pixelJson.put("red", red);
                pixelJson.put("green", green);
                pixelJson.put("blue", blue);

                row.put(pixelJson);
            }
            pixelArray.put(row);
        }

        jsonObject.put("pixels", pixelArray);
        return jsonObject;
    }


}
