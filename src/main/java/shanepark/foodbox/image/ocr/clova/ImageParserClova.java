package shanepark.foodbox.image.ocr.clova;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Component;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.ImageParser;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Clock;
import java.util.*;
import java.util.regex.Pattern;

@RequiredArgsConstructor
@Component
public class ImageParserClova implements ImageParser {
    private final ImageMarginCalculator imageMarginCalculator;
    private final NaverClovaApi naverClovaApi;
    private final Base64.Encoder encoder = Base64.getEncoder();
    private final Gson gson = new Gson();
    private final Clock clock;

    final Pattern DATE_PATTERN = Pattern.compile("\\d{1,2}일");
    final Pattern DAY_PATTERN = Pattern.compile("MON|TUE|WED|THU|FRI");

    @Override
    public List<ParsedMenu> parse(Path path) throws IOException {
        String base64 = encoder.encodeToString(Files.readAllBytes(path));
        String response = naverClovaApi.clovaRequest(base64);
        JsonObject jsonObject = gson.fromJson(response, JsonObject.class);
        JsonArray images = jsonObject.getAsJsonArray("images");
        JsonObject image = images.get(0).getAsJsonObject();
        JsonArray fields = image.getAsJsonArray("fields");

        BufferedImage bufferedImage = ImageIO.read(path.toFile());
        List<DayRegion> dayRegions = imageMarginCalculator.calcParseRegions(bufferedImage, fields);
        return parseResponse(fields, dayRegions);
    }

    private List<ParsedMenu> parseResponse(JsonArray fields, List<DayRegion> dayRegions) {
        Map<DayRegion, String> dateMap = new HashMap<>();
        Map<DayRegion, List<InferTextField>> menuMap = new HashMap<>();
        for (DayRegion dayRegion : dayRegions) {
            menuMap.put(dayRegion, new ArrayList<>());
        }

        for (int i = 0; i < fields.size(); i++) {
            JsonObject field = fields.get(i).getAsJsonObject();
            JsonArray vertices = field
                    .getAsJsonObject("boundingPoly")
                    .getAsJsonArray("vertices");

            int middleOfX = 0;
            int middleOfY = 0;
            for (int j = 0; j < 4; j++) {
                middleOfX += vertices.get(j).getAsJsonObject().get("x").getAsInt();
                middleOfY += vertices.get(j).getAsJsonObject().get("y").getAsInt();
            }
            middleOfX /= 4;
            middleOfY /= 4;

            String inferText = field.get("inferText").getAsString();
            float inferConfidence = field.get("inferConfidence").getAsFloat();
            if (inferConfidence <= 0.8 || inferText.isEmpty()) {
                continue;
            }

            for (DayRegion dayRegion : dayRegions) {
                if (dayRegion.date().contains(middleOfX, middleOfY)) {
                    if (!DATE_PATTERN.matcher(inferText).find() && !DAY_PATTERN.matcher(inferText).find()) {
                        continue;
                    }
                    dateMap.merge(dayRegion, inferText, String::concat);
                    break;
                }
                if (dayRegion.menu().contains(middleOfX, middleOfY)) {
                    InferTextField inferTextField = new InferTextField(middleOfY, middleOfX, inferText);
                    menuMap.get(dayRegion).add(inferTextField);
                    break;
                }
            }
        }

        List<ParsedMenu> answer = new ArrayList<>();
        for (DayRegion day : dayRegions) {
            ParsedMenu parsedMenu = new ParsedMenu(clock, dateMap.get(day));
            String menu = buildMenuString(menuMap.get(day));
            parsedMenu.setMenu(menu);
            answer.add(parsedMenu);
        }

        return answer;
    }

    private static String buildMenuString(List<InferTextField> inferTextFields) {
        int lastY = -1;
        StringBuilder menuBuilder = new StringBuilder();
        for (InferTextField inferTextField : inferTextFields) {
            if (Math.abs(inferTextField.y - lastY) > 10) {
                if (!menuBuilder.isEmpty()) {
                    menuBuilder.append("\n");
                }
            } else {
                menuBuilder.append(" ");
            }

            menuBuilder.append(inferTextField.inferText);
            lastY = inferTextField.y;
        }
        return menuBuilder.toString();
    }

    private record InferTextField(int y, int x, String inferText) {
    }

}
