package shanepark.foodbox.image.ocr.clova;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Component;
import shanepark.foodbox.image.domain.DayRegion;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.domain.ParsedMenuEiso;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.ImageParser;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Clock;
import java.time.LocalDate;
import java.util.*;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

@Slf4j
@RequiredArgsConstructor
@Component
public class ImageParserClovaEiso implements ImageParser {
    private final ImageMarginCalculator imageMarginCalculator;
    private final NaverClovaApi naverClovaApi;
    private final Base64.Encoder encoder = Base64.getEncoder();
    private final Gson gson = new Gson();
    private final Clock clock;

    final Pattern DATE_PATTERN = Pattern.compile("\\d{1,2}월\\s*\\d{1,2}일");
    final Pattern DAY_NUMBER_PATTERN = Pattern.compile("\\d{1,2}");

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
        Map<DayRegion, List<InferTextField>> menuMap = initializeMenuMap(dayRegions);
        Map<DayRegion, List<DateTextField>> dateTextMap = initializeDateTextMap(dayRegions);

        populateFields(fields, dayRegions, menuMap, dateTextMap);
        mergeDateTexts(dayRegions, dateTextMap, dateMap);

        List<ParsedMenu> parsedMenus = new ArrayList<>();
        for (DayRegion day : dayRegions) {
            String dateStr = dateMap.get(day);
            if (dateStr == null || dateStr.isEmpty()) {
                continue;
            }
            try {
                ParsedMenuEiso parsedMenuEiso = new ParsedMenuEiso(clock, dateStr);
                String menu = buildMenuString(menuMap.get(day));
                parsedMenuEiso.setMenu(menu);
                parsedMenus.add(new ParsedMenu(parsedMenuEiso.getDate(), parsedMenuEiso.getMenus()));
            } catch (IllegalArgumentException e) {
                log.warn("Failed to parse date: {}", dateStr, e);
            }
        }

        return parsedMenus;
    }

    private Map<DayRegion, List<InferTextField>> initializeMenuMap(List<DayRegion> dayRegions) {
        Map<DayRegion, List<InferTextField>> menuMap = new HashMap<>();
        for (DayRegion dayRegion : dayRegions) {
            menuMap.put(dayRegion, new ArrayList<>());
        }
        return menuMap;
    }

    private Map<DayRegion, List<DateTextField>> initializeDateTextMap(List<DayRegion> dayRegions) {
        Map<DayRegion, List<DateTextField>> dateTextMap = new HashMap<>();
        for (DayRegion dayRegion : dayRegions) {
            dateTextMap.put(dayRegion, new ArrayList<>());
        }
        return dateTextMap;
    }

    private void populateFields(JsonArray fields,
                                List<DayRegion> dayRegions,
                                Map<DayRegion, List<InferTextField>> menuMap,
                                Map<DayRegion, List<DateTextField>> dateTextMap) {
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
                    if (isDateToken(inferText)) {
                        dateTextMap.get(dayRegion).add(new DateTextField(middleOfX, middleOfY, inferText));
                    }
                    break;
                }
                if (dayRegion.menu().contains(middleOfX, middleOfY)) {
                    menuMap.get(dayRegion).add(new InferTextField(middleOfY, middleOfX, inferText));
                    break;
                }
            }
        }
    }

    private boolean isDateToken(String inferText) {
        return DATE_PATTERN.matcher(inferText).find()
                || inferText.matches("\\d{1,2}월")
                || inferText.matches("\\d{1,2}일")
                || isDayNumberToken(inferText);
    }

    private boolean isDayNumberToken(String inferText) {
        if (!DAY_NUMBER_PATTERN.matcher(inferText).matches()) {
            return false;
        }
        int day = Integer.parseInt(inferText);
        return day >= 1 && day <= 31;
    }

    private void mergeDateTexts(List<DayRegion> dayRegions,
                                Map<DayRegion, List<DateTextField>> dateTextMap,
                                Map<DayRegion, String> dateMap) {
        for (DayRegion dayRegion : dayRegions) {
            List<DateTextField> dateTexts = dateTextMap.get(dayRegion);
            if (dateTexts.isEmpty()) {
                continue;
            }
            dateTexts.sort(Comparator.comparingInt(d -> d.x));
            String mergedDate = dateTexts.stream()
                    .map(dateText -> dateText.text)
                    .collect(Collectors.joining(" "))
                    .replaceAll("\\s+", " ");
            if (isDayNumberToken(mergedDate)) {
                int month = LocalDate.now(clock).getMonthValue();
                int day = Integer.parseInt(mergedDate);
                mergedDate = String.format("%02d월 %02d일", month, day);
            }
            dateMap.put(dayRegion, mergedDate);
        }
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

    private record DateTextField(int x, int y, String text) {
    }

}
