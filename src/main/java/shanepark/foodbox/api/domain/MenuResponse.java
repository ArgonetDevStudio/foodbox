package shanepark.foodbox.api.domain;

import java.util.List;

public record MenuResponse(String date, List<String> menus, boolean isValid) {
    public static MenuResponse of(Menu menu) {
        return new MenuResponse(menu.getDate().toString(), menu.getMenus(), true);
    }

}
