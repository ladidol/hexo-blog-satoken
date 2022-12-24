package org.cuit.epoch.current;

import org.cuit.epoch.util.Result;
import org.springframework.boot.web.servlet.error.ErrorController;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;
import java.io.IOException;

/**
 * 处理 404
 * @author kong
 */
@RestController
public class NotFoundHandle implements ErrorController {

	@RequestMapping("/error")
    public Object error(HttpServletRequest request, HttpServletResponse response) throws IOException {
//		response.setStatus(200);
        // 2022/11/25 不需要设置一下status,直接用原始的就行
//        return SaResult.get(404, "not found", null);
        return Result.fail(404,"not found");
    }

    @Override
    public String getErrorPath() {
        return null;
    }
}
