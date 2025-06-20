package shanepark.foodbox.image.domain;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import lombok.Getter;

import java.util.regex.Pattern;

@Getter
public class GridParser {
    private int menu1DateStart = -1;
    private int menu1DateEnd = -1;
    private int menu2DateStart = Integer.MAX_VALUE;
    private int menu2DateEnd = -1;

    public static final int GAP = 2;

    private final Pattern DATE_PATTERN = Pattern.compile("\\d{1,2}일");
    private final Pattern DAY_PATTERN = Pattern.compile("MON|TUE|WED|THU|FRI");

    private JsonArray mondayVertices;
    private JsonArray tuesdayVertices;
    private final int width;
    private final int startX;

    public GridParser(JsonArray fields) {
        for (JsonElement e : fields) {
            JsonObject field = e.getAsJsonObject();
            String inferText = field.get("inferText").getAsString();
            if (DAY_PATTERN.matcher(inferText).find()) {
                processDayPattern(field);
                continue;
            }
            if (DATE_PATTERN.matcher(inferText).find()) {
                processDate(field);
            }
        }
        if (mondayVertices == null || tuesdayVertices == null) {
            throw new IllegalStateException("월요일 또는 화요일 패턴이 발견되지 않았습니다.");
        }
        int midOfMonday = getMidX(mondayVertices);
        int midOfTuesday = getMidX(tuesdayVertices);
        width = midOfTuesday - midOfMonday - GAP;
        startX = midOfMonday - (width / 2) - GAP;
    }

    public void processDayPattern(JsonObject field) {
        JsonArray vertices = getVertices(field);
        JsonObject leftTop = vertices.get(0).getAsJsonObject();
        String inferText = field.get("inferText").getAsString();
        if (inferText.startsWith("월") && mondayVertices == null) {
            mondayVertices = vertices;
        }
        if (inferText.startsWith("화") && tuesdayVertices == null) {
            tuesdayVertices = vertices;
        }

        int y = leftTop.get("y").getAsInt();
        if (menu1DateStart == -1) {
            menu1DateStart = y;
            return;
        }
        if (Math.abs(y - menu1DateStart) < 100) {
            menu1DateStart = Math.min(menu1DateStart, y);
            return;
        }
        menu2DateStart = Math.min(menu2DateStart, y);
    }

    public void processDate(JsonObject field) {
        JsonObject rightBottom = getVertices(field).get(3).getAsJsonObject();
        int y = rightBottom.get("y").getAsInt();
        if (menu1DateEnd == -1) {
            menu1DateEnd = y;
            return;
        }
        if (Math.abs(y - menu1DateEnd) < 100) {
            menu1DateEnd = Math.max(menu1DateEnd, y);
            return;
        }
        menu2DateEnd = Math.max(menu2DateEnd, y);
    }

    public int getRow1Start() {
        return menu1DateStart - 1;
    }

    public int getRow1DateHeight() {
        return menu1DateEnd - menu1DateStart;
    }

    public int getRow2Start() {
        return menu2DateStart - 3;
    }

    public int getRow2DateHeight() {
        return menu2DateEnd - menu2DateStart;
    }

    private static JsonArray getVertices(JsonObject field) {
        return field
                .getAsJsonObject("boundingPoly")
                .getAsJsonArray("vertices");
    }

    private int getMidX(JsonArray mondayVertices) {
        JsonObject leftTop = mondayVertices.get(0).getAsJsonObject();
        JsonObject rightTop = mondayVertices.get(1).getAsJsonObject();
        int leftX = leftTop.get("x").getAsInt();
        int rightX = rightTop.get("x").getAsInt();
        return (leftX + rightX) / 2;
    }

}
