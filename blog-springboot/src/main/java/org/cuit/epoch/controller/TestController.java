package org.cuit.epoch.controller;

import cn.dev33.satoken.stp.StpUtil;
import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import lombok.extern.slf4j.Slf4j;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * @author: ladidol
 * @date: 2022/11/18 17:42
 * @description:
 */
@RestController
@Slf4j
@Api(tags = "测试模块")
public class TestController {

    @GetMapping("/admin/test")
    @ApiOperation(value = "test1")
    public String test1(String username, String password) {
        return StpUtil.getLoginId() + "成功了";
    }

}